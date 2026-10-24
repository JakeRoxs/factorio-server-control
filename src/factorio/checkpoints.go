package factorio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/OpenFactorioServerManager/factorio-server-manager/bootstrap"
	"github.com/OpenFactorioServerManager/rcon"
)

const (
	checkpointSchemaVersion = 1
	checkpointStateFile     = "state.json"
	checkpointFilesDir      = "files"

	CheckpointTriggerManual     = "manual"
	CheckpointTriggerInterval   = "interval"
	CheckpointTriggerLastPlayer = "last-player"
	CheckpointTriggerCleanStop  = "clean-stop"

	defaultCheckpointIntervalMinutes = 30
	defaultCheckpointRetention       = 10
	minimumCheckpointIntervalMinutes = 5
	maximumCheckpointIntervalMinutes = 7 * 24 * 60
	maximumCheckpointRetention       = 1000
)

var (
	ErrCheckpointNotFound       = errors.New("checkpoint not found")
	ErrInvalidCheckpointSetting = errors.New("invalid checkpoint settings")
	ErrCheckpointServerActive   = errors.New("stop Factorio before restoring a checkpoint")
	ErrCheckpointCooldown       = errors.New("an automated checkpoint was created recently")
)

type CheckpointSettings struct {
	IntervalEnabled   bool `json:"interval_enabled"`
	IntervalMinutes   int  `json:"interval_minutes"`
	LastPlayerEnabled bool `json:"last_player_enabled"`
	CleanStopEnabled  bool `json:"clean_stop_enabled"`
	RetentionCount    int  `json:"retention_count"`
}

type Checkpoint struct {
	ID         string    `json:"id"`
	FileName   string    `json:"file_name"`
	CreatedAt  time.Time `json:"created_at"`
	Trigger    string    `json:"trigger"`
	Size       int64     `json:"size"`
	SourceSave string    `json:"source_save,omitempty"`
}

type CheckpointState struct {
	ProfileID        string             `json:"profile_id"`
	Settings         CheckpointSettings `json:"settings"`
	Checkpoints      []Checkpoint       `json:"checkpoints"`
	LastError        string             `json:"last_error,omitempty"`
	LastErrorAt      *time.Time         `json:"last_error_at,omitempty"`
	LastCheckpointAt *time.Time         `json:"last_checkpoint_at,omitempty"`
}

type checkpointStore struct {
	SchemaVersion    int                `json:"schema_version"`
	Settings         CheckpointSettings `json:"settings"`
	LastError        string             `json:"last_error,omitempty"`
	LastErrorAt      *time.Time         `json:"last_error_at,omitempty"`
	LastCheckpointAt *time.Time         `json:"last_checkpoint_at,omitempty"`
}

type checkpointMetadata struct {
	SchemaVersion int       `json:"schema_version"`
	ID            string    `json:"id"`
	CreatedAt     time.Time `json:"created_at"`
	Trigger       string    `json:"trigger"`
	SourceSave    string    `json:"source_save,omitempty"`
}

var checkpointOperationMutex sync.Mutex
var checkpointMonitorStates = make(map[string]*checkpointMonitorState)
var checkpointMonitorStatesMu sync.Mutex
var checkpointNow = time.Now
var checkpointMonitorPollInterval = 15 * time.Second
var checkpointAutomatedCooldown = 2 * time.Minute
var checkpointRootPath = func() string {
	return filepath.Join(filepath.Dir(bootstrap.GetConfig().ConfFile), "checkpoints")
}
var checkpointSavesPath = func() string { return profileActiveDirectories()["saves"] }
var checkpointRunRCON = runCheckpointRCONCommand
var checkpointCreateLiveSave = createLiveCheckpointSave
var checkpointCreateLiveSaveForServer = createLiveCheckpointSaveForServer
var checkpointRunRCONForServer = runCheckpointRCONCommandForServer
var checkpointIDPattern = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}\.[0-9]{9}Z-(manual|interval|last-player|clean-stop)(-[0-9]+)?$`)
var connectedPlayerCountPattern = regexp.MustCompile(`(?im)^[ \t]*online players[ \t]*\([ \t]*([0-9]+)[ \t]*\)[ \t]*:?[ \t]*\r?$`)

func defaultCheckpointSettings() CheckpointSettings {
	return CheckpointSettings{
		IntervalMinutes: defaultCheckpointIntervalMinutes,
		RetentionCount:  defaultCheckpointRetention,
	}
}

// GetCheckpointState returns checkpoint state for the default server.
func GetCheckpointState() (CheckpointState, error) {
	return GetCheckpointStateForServer(GetFactorioServer())
}

// UpdateCheckpointSettings saves checkpoint settings for the default server.
func UpdateCheckpointSettings(settings CheckpointSettings) (CheckpointState, error) {
	return UpdateCheckpointSettingsForServer(GetFactorioServer(), settings)
}

// CreateCheckpoint creates a checkpoint for the default server.
func CreateCheckpoint(trigger string) (CheckpointState, error) {
	return CreateCheckpointForServer(GetFactorioServer(), trigger)
}

// CreateCheckpointForServer creates a fixed checkpoint archive for the named
// server. The snapshot source, RCON connection, archive store, settings and
// retention are all bound to that server.
func CreateCheckpointForServer(server *Server, trigger string) (CheckpointState, error) {
	checkpointOperationMutex.Lock()
	defer checkpointOperationMutex.Unlock()

	if !validCheckpointTrigger(trigger) {
		return CheckpointState{}, fmt.Errorf("invalid checkpoint trigger %q", trigger)
	}
	storeID := checkpointServerStoreID(server.ID)
	store, err := loadCheckpointStore(storeID)
	if err != nil {
		return CheckpointState{}, err
	}
	now := checkpointNow().UTC()
	if trigger != CheckpointTriggerManual && store.LastCheckpointAt != nil && now.Sub(*store.LastCheckpointAt) < checkpointAutomatedCooldown {
		return checkpointStateForServer(server)
	}

	id, err := newCheckpointID(storeID, trigger, now)
	if err != nil {
		return CheckpointState{}, err
	}
	path, sourceSave, cleanup, err := checkpointSourceForServer(server, id)
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		_ = recordCheckpointErrorUnlocked(storeID, err)
		return CheckpointState{}, err
	}

	metadata := checkpointMetadata{
		SchemaVersion: checkpointSchemaVersion,
		ID:            id,
		CreatedAt:     now,
		Trigger:       trigger,
		SourceSave:    sourceSave,
	}
	if err := persistCheckpoint(storeID, metadata, path); err != nil {
		_ = recordCheckpointErrorUnlocked(storeID, err)
		return CheckpointState{}, err
	}
	store.LastCheckpointAt = &now
	store.LastError = ""
	store.LastErrorAt = nil
	if err := saveCheckpointStore(storeID, store); err != nil {
		return CheckpointState{}, err
	}
	if err := enforceCheckpointRetention(storeID, store.Settings.RetentionCount); err != nil {
		log.Printf("Checkpoint %s was created, but retention cleanup failed: %v", id, err)
		_ = recordCheckpointErrorUnlocked(storeID, err)
	}
	return checkpointStateForServer(server)
}

// RestoreCheckpointForServer restores a fixed checkpoint into the named server's
// save directory after verifying the archive and enforcing the stopped guard.
func RestoreCheckpointForServer(server *Server, id string) (Save, error) {
	checkpointOperationMutex.Lock()
	defer checkpointOperationMutex.Unlock()

	if server.GetRunning() || server.IsStopping() {
		return Save{}, ErrCheckpointServerActive
	}
	storeID := checkpointServerStoreID(server.ID)
	checkpoint, path, err := findCheckpoint(storeID, id)
	if err != nil {
		return Save{}, err
	}
	if err := verifyCheckpointZip(path); err != nil {
		return Save{}, fmt.Errorf("verify checkpoint: %w", err)
	}

	destinationName, err := availableRestoredSaveNameForServer(server, checkpoint.ID)
	if err != nil {
		return Save{}, err
	}
	destinationPath := filepath.Join(server.SavesDir, destinationName)
	if err := copyCheckpointAtomically(path, destinationPath); err != nil {
		return Save{}, fmt.Errorf("restore checkpoint: %w", err)
	}
	info, err := os.Stat(destinationPath)
	if err != nil {
		return Save{}, err
	}
	return Save{Name: destinationName, LastMod: info.ModTime(), Size: info.Size()}, nil
}

// RestoreCheckpoint restores a checkpoint for the default server.
func RestoreCheckpoint(id string) (Save, error) {
	return RestoreCheckpointForServer(GetFactorioServer(), id)
}

// DeleteCheckpoint deletes a checkpoint for the default server.
func DeleteCheckpoint(id string) (CheckpointState, error) {
	return DeleteCheckpointForServer(GetFactorioServer(), id)
}

// ListCheckpointsForServer lists the fixed checkpoints for the named server.
func ListCheckpointsForServer(server *Server) (CheckpointState, error) {
	return GetCheckpointStateForServer(server)
}

// FindCheckpointFileForServer locates the archive for the named server's checkpoint.
func FindCheckpointFileForServer(server *Server, id string) (Checkpoint, string, error) {
	checkpointOperationMutex.Lock()
	defer checkpointOperationMutex.Unlock()
	storeID := checkpointServerStoreID(server.ID)
	return findCheckpoint(storeID, id)
}

// DeleteCheckpointForServer deletes the named server's checkpoint.
func DeleteCheckpointForServer(server *Server, id string) (CheckpointState, error) {
	checkpointOperationMutex.Lock()
	defer checkpointOperationMutex.Unlock()

	storeID := checkpointServerStoreID(server.ID)
	checkpoint, path, err := findCheckpoint(storeID, id)
	if err != nil {
		return CheckpointState{}, err
	}
	if err := os.Remove(path); err != nil {
		return CheckpointState{}, err
	}
	if err := os.Remove(checkpointMetadataPath(storeID, checkpoint.ID)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return CheckpointState{}, err
	}
	remaining, err := listCheckpoints(storeID)
	if err != nil {
		return CheckpointState{}, err
	}
	store, err := loadCheckpointStore(storeID)
	if err != nil {
		return CheckpointState{}, err
	}
	if len(remaining) == 0 {
		store.LastCheckpointAt = nil
	} else {
		latest := remaining[0].CreatedAt.UTC()
		store.LastCheckpointAt = &latest
	}
	if err := saveCheckpointStore(storeID, store); err != nil {
		return CheckpointState{}, err
	}
	return checkpointStateForServer(server)
}

// FindCheckpointFile locates a checkpoint archive for the default server.
func FindCheckpointFile(id string) (Checkpoint, string, error) {
	return FindCheckpointFileForServer(GetFactorioServer(), id)
}

func runCleanStopCheckpoint(server *Server) {
	state, err := GetCheckpointStateForServer(server)
	if err != nil {
		log.Printf("Unable to load clean-stop checkpoint settings: %v", err)
		return
	}
	if !state.Settings.CleanStopEnabled {
		return
	}
	if _, err := CreateCheckpointForServer(server, CheckpointTriggerCleanStop); err != nil && !errors.Is(err, ErrCheckpointCooldown) {
		log.Printf("Unable to create clean-stop checkpoint: %v", err)
	}
}

// checkpointMonitorState tracks one checkpoint monitor per server so a running
// server never shares its schedule with a secondary server.
type checkpointMonitorState struct {
	serverID string
	cancel   context.CancelFunc
}

// checkpointMonitorKey returns the per-server identity used for checkpoint
// monitor state. Empty and the default server share the canonical identity.
func checkpointMonitorKey(serverID string) string {
	if serverID == "" {
		return DefaultServerID
	}
	return serverID
}

func startCheckpointMonitor(server *Server) {
	key := checkpointMonitorKey(server.ID)
	checkpointMonitorStatesMu.Lock()
	if state, exists := checkpointMonitorStates[key]; exists && state.cancel != nil {
		checkpointMonitorStatesMu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	checkpointMonitorStates[key] = &checkpointMonitorState{serverID: server.ID, cancel: cancel}
	checkpointMonitorStatesMu.Unlock()
	go checkpointMonitorLoop(ctx, key, server)
}

func stopCheckpointMonitor(serverID string) {
	key := checkpointMonitorKey(serverID)
	checkpointMonitorStatesMu.Lock()
	if state, exists := checkpointMonitorStates[key]; exists && state.cancel != nil {
		state.cancel()
		delete(checkpointMonitorStates, key)
	}
	checkpointMonitorStatesMu.Unlock()
}

func checkpointMonitorLoop(ctx context.Context, key string, server *Server) {
	startedAt := checkpointNow().UTC()
	var previousPlayers *int
	ticker := time.NewTicker(checkpointMonitorPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !server.GetRunning() || server.IsStopping() {
				return
			}
			unlock := LockProfileDataRead()
			state, err := GetCheckpointStateForServer(server)
			if err != nil {
				unlock()
				log.Printf("Unable to load checkpoint schedule: %v", err)
				continue
			}

			now := checkpointNow().UTC()
			lastIntervalBase := startedAt
			if state.LastCheckpointAt != nil && state.LastCheckpointAt.After(lastIntervalBase) {
				lastIntervalBase = *state.LastCheckpointAt
			}
			if state.Settings.IntervalEnabled && now.Sub(lastIntervalBase) >= time.Duration(state.Settings.IntervalMinutes)*time.Minute {
				if _, err := CreateCheckpointForServer(server, CheckpointTriggerInterval); err != nil && !errors.Is(err, ErrCheckpointCooldown) {
					log.Printf("Unable to create scheduled checkpoint: %v", err)
				}
			}

			if state.Settings.LastPlayerEnabled {
				players, playerErr := serverConnectedPlayerCount(server)
				if playerErr != nil {
					log.Printf("Unable to read connected player count for checkpoints: %v", playerErr)
				} else {
					if previousPlayers != nil && *previousPlayers > 0 && players == 0 {
						if _, err := CreateCheckpointForServer(server, CheckpointTriggerLastPlayer); err != nil && !errors.Is(err, ErrCheckpointCooldown) {
							log.Printf("Unable to create last-player checkpoint: %v", err)
						}
					}
					previousPlayers = &players
				}
			} else {
				previousPlayers = nil
			}
			unlock()
		}
	}
}

func connectedPlayerCount() (int, error) {
	// Use Factorio's built-in multiplayer command. Lua console commands such as
	// /silent-command permanently disable achievements for the live save.
	response, err := checkpointRunRCON(`/players online count`)
	if err != nil {
		return 0, err
	}
	return parseConnectedPlayerCount(response)
}

func serverConnectedPlayerCount(server *Server) (int, error) {
	response, err := checkpointRunRCONForServer(server, `/players online count`)
	if err != nil {
		return 0, err
	}
	return parseConnectedPlayerCount(response)
}

// GetCheckpointStateForServer returns checkpoint state scoped to the named
// server. Each server owns its own checkpoint archives, settings, retention
// and error state.
func GetCheckpointStateForServer(server *Server) (CheckpointState, error) {
	checkpointOperationMutex.Lock()
	defer checkpointOperationMutex.Unlock()
	return checkpointStateForServer(server)
}

// UpdateCheckpointSettingsForServer saves checkpoint settings scoped to the
// named server.
func UpdateCheckpointSettingsForServer(server *Server, settings CheckpointSettings) (CheckpointState, error) {
	checkpointOperationMutex.Lock()
	defer checkpointOperationMutex.Unlock()

	if err := validateCheckpointSettings(settings); err != nil {
		return CheckpointState{}, err
	}
	storeID := checkpointServerStoreID(server.ID)
	store, err := loadCheckpointStore(storeID)
	if err != nil {
		return CheckpointState{}, err
	}
	store.Settings = settings
	if err := saveCheckpointStore(storeID, store); err != nil {
		return CheckpointState{}, err
	}
	return checkpointStateForServer(server)
}

func checkpointStateForServer(server *Server) (CheckpointState, error) {
	storeID := checkpointServerStoreID(server.ID)
	store, err := loadCheckpointStore(storeID)
	if err != nil {
		return CheckpointState{}, err
	}
	checkpoints, err := listCheckpoints(storeID)
	if err != nil {
		return CheckpointState{}, err
	}
	return CheckpointState{
		ProfileID:        server.ID,
		Settings:         store.Settings,
		Checkpoints:      checkpoints,
		LastError:        store.LastError,
		LastErrorAt:      store.LastErrorAt,
		LastCheckpointAt: store.LastCheckpointAt,
	}, nil
}

func parseConnectedPlayerCount(response string) (int, error) {
	matches := connectedPlayerCountPattern.FindAllStringSubmatch(response, -1)
	if len(matches) != 1 {
		return 0, fmt.Errorf("unexpected player count %q", response)
	}
	players, err := strconv.Atoi(matches[0][1])
	if err != nil || players < 0 {
		return 0, fmt.Errorf("unexpected player count %q", response)
	}
	return players, nil
}

func getCheckpointStateUnlocked() (CheckpointState, error) {
	profileID, err := activeProfileID()
	if err != nil {
		return CheckpointState{}, err
	}
	return checkpointStateForProfile(profileID)
}

func checkpointStateForProfile(profileID string) (CheckpointState, error) {
	store, err := loadCheckpointStore(profileID)
	if err != nil {
		return CheckpointState{}, err
	}
	checkpoints, err := listCheckpoints(profileID)
	if err != nil {
		return CheckpointState{}, err
	}
	return CheckpointState{
		ProfileID:        profileID,
		Settings:         store.Settings,
		Checkpoints:      checkpoints,
		LastError:        store.LastError,
		LastErrorAt:      store.LastErrorAt,
		LastCheckpointAt: store.LastCheckpointAt,
	}, nil
}

func activeProfileID() (string, error) {
	profileMutex.Lock()
	defer profileMutex.Unlock()
	manifest, err := loadProfileManifest()
	if err != nil {
		return "", err
	}
	if err := validateProfileID(manifest.ActiveProfileID); err != nil {
		return "", errors.New("active profile metadata is invalid")
	}
	return manifest.ActiveProfileID, nil
}

func validateCheckpointSettings(settings CheckpointSettings) error {
	if settings.IntervalMinutes < minimumCheckpointIntervalMinutes || settings.IntervalMinutes > maximumCheckpointIntervalMinutes {
		return fmt.Errorf("%w: interval must be between %d and %d minutes", ErrInvalidCheckpointSetting, minimumCheckpointIntervalMinutes, maximumCheckpointIntervalMinutes)
	}
	if settings.RetentionCount < 0 || settings.RetentionCount > maximumCheckpointRetention {
		return fmt.Errorf("%w: retention must be between 0 and %d", ErrInvalidCheckpointSetting, maximumCheckpointRetention)
	}
	return nil
}

func validCheckpointTrigger(trigger string) bool {
	switch trigger {
	case CheckpointTriggerManual, CheckpointTriggerInterval, CheckpointTriggerLastPlayer, CheckpointTriggerCleanStop:
		return true
	default:
		return false
	}
}

func checkpointProfileDirectory(profileID string) string {
	return filepath.Join(checkpointRootPath(), profileID)
}

func checkpointFilesDirectory(profileID string) string {
	return filepath.Join(checkpointProfileDirectory(profileID), checkpointFilesDir)
}

func checkpointMetadataPath(profileID, id string) string {
	return filepath.Join(checkpointFilesDirectory(profileID), id+".json")
}

func loadCheckpointStore(profileID string) (checkpointStore, error) {
	store := checkpointStore{SchemaVersion: checkpointSchemaVersion, Settings: defaultCheckpointSettings()}
	contents, err := os.ReadFile(filepath.Join(checkpointProfileDirectory(profileID), checkpointStateFile))
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return checkpointStore{}, err
	}
	if err := json.Unmarshal(contents, &store); err != nil {
		return checkpointStore{}, fmt.Errorf("decode checkpoint settings: %w", err)
	}
	if store.SchemaVersion != checkpointSchemaVersion {
		return checkpointStore{}, fmt.Errorf("unsupported checkpoint schema version %d", store.SchemaVersion)
	}
	if err := validateCheckpointSettings(store.Settings); err != nil {
		return checkpointStore{}, err
	}
	return store, nil
}

func saveCheckpointStore(profileID string, store checkpointStore) error {
	root := checkpointProfileDirectory(profileID)
	if err := os.MkdirAll(root, 0700); err != nil {
		return err
	}
	store.SchemaVersion = checkpointSchemaVersion
	return writeCheckpointJSONAtomically(filepath.Join(root, checkpointStateFile), store)
}

func writeCheckpointJSONAtomically(path string, value interface{}) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".checkpoint-json-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	encoder := json.NewEncoder(temporary)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Chmod(0600); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

func newCheckpointID(profileID, trigger string, now time.Time) (string, error) {
	base := now.Format("20060102T150405.000000000Z") + "-" + trigger
	for suffix := 0; suffix < 1000; suffix++ {
		id := base
		if suffix > 0 {
			id += fmt.Sprintf("-%d", suffix)
		}
		if _, err := os.Stat(filepath.Join(checkpointFilesDirectory(profileID), id+".zip")); errors.Is(err, os.ErrNotExist) {
			return id, nil
		} else if err != nil {
			return "", err
		}
	}
	return "", errors.New("unable to allocate a unique checkpoint name")
}

func checkpointSource(id string) (path, sourceSave string, cleanup func(), err error) {
	server := GetFactorioServer()
	if server.GetRunning() {
		path, err = checkpointCreateLiveSave(id)
		if err != nil {
			return "", "", nil, err
		}
		sourceSave = server.Snapshot().Savefile
		cleanup = func() { _ = os.Remove(path) }
		return path, sourceSave, cleanup, nil
	}

	snapshot := server.Snapshot()
	save, err := findCheckpointSourceSave(snapshot.Savefile)
	if err != nil {
		return "", "", nil, fmt.Errorf("find source save: %w", err)
	}
	return filepath.Join(checkpointSavesPath(), save.Name), save.Name, nil, nil
}

func findCheckpointSourceSave(selected string) (*Save, error) {
	entries, err := os.ReadDir(checkpointSavesPath())
	if err != nil {
		return nil, err
	}
	var latest *Save
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		if !isUsableSave(info) {
			continue
		}
		save := &Save{Name: info.Name(), LastMod: info.ModTime(), Size: info.Size()}
		if selected != "" && !strings.HasPrefix(selected, "Load Latest") && save.Name == selected {
			return save, nil
		}
		if latest == nil || latest.LastMod.Before(save.LastMod) {
			latest = save
		}
	}
	if selected != "" && !strings.HasPrefix(selected, "Load Latest") {
		return nil, errors.New("selected save not found")
	}
	if latest == nil {
		return nil, errors.New("no usable save file found")
	}
	return latest, nil
}

// Per-server checkpoint helpers bind the snapshot source, RCON connection and
// restored destination to the named server rather than the process-wide default
// runtime. The checkpoint archive store itself remains profile-bound because
// profiles are reusable presets, not server identities; the per-server binding
// applies to the snapshot target, RCON credentials and restore destination.

func checkpointSourceForServer(server *Server, id string) (path, sourceSave string, cleanup func(), err error) {
	if server.GetRunning() {
		path, err = checkpointCreateLiveSaveForServer(server, id)
		if err != nil {
			return "", "", nil, err
		}
		return path, server.Snapshot().Savefile, func() { _ = os.Remove(path) }, nil
	}
	snapshot := server.Snapshot()
	save, err := findCheckpointSourceSaveForServer(server, snapshot.Savefile)
	if err != nil {
		return "", "", nil, fmt.Errorf("find source save: %w", err)
	}
	return filepath.Join(server.SavesDir, save.Name), save.Name, nil, nil
}

func createLiveCheckpointSaveForServer(server *Server, id string) (string, error) {
	if err := waitForCheckpointRCONReady(server, 60*time.Second); err != nil {
		return "", err
	}
	baseName := "fsm-checkpoint-" + strings.ReplaceAll(strings.SplitN(id, "-", 2)[0], ".", "")
	path := filepath.Join(server.SavesDir, baseName+".zip")
	_ = os.Remove(path)
	if _, err := checkpointRunRCONForServer(server, "/save "+baseName); err != nil {
		return "", fmt.Errorf("ask Factorio to save: %w", err)
	}
	if err := waitForCompleteCheckpointSave(path, 90*time.Second); err != nil {
		return "", err
	}
	return path, nil
}

func runCheckpointRCONCommandForServer(server *Server, command string) (string, error) {
	address, password := server.rconConnectionSettings()
	console, err := rcon.Dial(address, password)
	if err != nil {
		return "", err
	}
	defer console.Close()
	requestID, err := console.Write(command)
	if err != nil {
		return "", err
	}
	response, responseID, err := console.Read()
	if err != nil {
		return "", err
	}
	if responseID != requestID {
		return "", fmt.Errorf("unexpected RCON response id %d for request %d", responseID, requestID)
	}
	return response, nil
}

func findCheckpointSourceSaveForServer(server *Server, selected string) (*Save, error) {
	entries, err := os.ReadDir(server.SavesDir)
	if err != nil {
		return nil, err
	}
	var latest *Save
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		if !isUsableSave(info) {
			continue
		}
		save := &Save{Name: info.Name(), LastMod: info.ModTime(), Size: info.Size()}
		if selected != "" && !strings.HasPrefix(selected, "Load Latest") && save.Name == selected {
			return save, nil
		}
		if latest == nil || latest.LastMod.Before(save.LastMod) {
			latest = save
		}
	}
	if selected != "" && !strings.HasPrefix(selected, "Load Latest") {
		return nil, errors.New("selected save not found")
	}
	if latest == nil {
		return nil, errors.New("no usable save file found")
	}
	return latest, nil
}

func availableRestoredSaveNameForServer(server *Server, id string) (string, error) {
	directory := server.SavesDir
	base := "restored-" + id
	for suffix := 0; suffix < 1000; suffix++ {
		name := base + ".zip"
		if suffix > 0 {
			name = fmt.Sprintf("%s-%d.zip", base, suffix)
		}
		if _, err := os.Stat(filepath.Join(directory, name)); errors.Is(err, os.ErrNotExist) {
			return name, nil
		} else if err != nil {
			return "", err
		}
	}
	return "", errors.New("unable to allocate a restored save name")
}

func createLiveCheckpointSave(id string) (string, error) {
	if err := waitForCheckpointRCONReady(GetFactorioServer(), 60*time.Second); err != nil {
		return "", err
	}
	baseName := "fsm-checkpoint-" + strings.ReplaceAll(strings.SplitN(id, "-", 2)[0], ".", "")
	path := filepath.Join(checkpointSavesPath(), baseName+".zip")
	_ = os.Remove(path)
	if _, err := checkpointRunRCON("/save " + baseName); err != nil {
		return "", fmt.Errorf("ask Factorio to save: %w", err)
	}
	if err := waitForCompleteCheckpointSave(path, 90*time.Second); err != nil {
		return "", err
	}
	return path, nil
}

func waitForCheckpointRCONReady(server *Server, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if server.isStartupReady() {
			return nil
		}
		if server.IsStopping() || !server.GetRunning() {
			return errors.New("Factorio stopped before RCON became ready")
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("Factorio RCON did not become ready within %s", timeout)
}

func runCheckpointRCONCommand(command string) (string, error) {
	address, password := GetFactorioServer().rconConnectionSettings()
	console, err := rcon.Dial(address, password)
	if err != nil {
		return "", err
	}
	defer console.Close()
	requestID, err := console.Write(command)
	if err != nil {
		return "", err
	}
	response, responseID, err := console.Read()
	if err != nil {
		return "", err
	}
	if responseID != requestID {
		return "", fmt.Errorf("unexpected RCON response id %d for request %d", responseID, requestID)
	}
	return response, nil
}

func waitForCompleteCheckpointSave(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	lastSize := int64(-1)
	stableReads := 0
	for time.Now().Before(deadline) {
		info, err := os.Lstat(path)
		if err == nil && info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 && info.Size() > 0 {
			if info.Size() == lastSize {
				stableReads++
			} else {
				lastSize = info.Size()
				stableReads = 0
			}
			if stableReads >= 2 && verifyCheckpointZip(path) == nil {
				return nil
			}
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("Factorio save %s did not become a complete ZIP within %s", filepath.Base(path), timeout)
}

func persistCheckpoint(profileID string, metadata checkpointMetadata, sourcePath string) error {
	if err := verifyCheckpointZip(sourcePath); err != nil {
		return fmt.Errorf("source save is incomplete: %w", err)
	}
	directory := checkpointFilesDirectory(profileID)
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	destinationPath := filepath.Join(directory, metadata.ID+".zip")
	if err := copyCheckpointAtomically(sourcePath, destinationPath); err != nil {
		return err
	}
	if err := writeCheckpointJSONAtomically(checkpointMetadataPath(profileID, metadata.ID), metadata); err != nil {
		_ = os.Remove(destinationPath)
		return err
	}
	return nil
}

func copyCheckpointAtomically(sourcePath, destinationPath string) error {
	info, err := os.Lstat(sourcePath)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("checkpoint source is not a regular file")
	}
	if err := os.MkdirAll(filepath.Dir(destinationPath), 0755); err != nil {
		return err
	}
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()
	temporary, err := os.CreateTemp(filepath.Dir(destinationPath), ".checkpoint-copy-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := io.Copy(temporary, source); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Chmod(0644); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := verifyCheckpointZip(temporaryPath); err != nil {
		return err
	}
	return os.Rename(temporaryPath, destinationPath)
}

func verifyCheckpointZip(path string) error {
	return verifyFactorioSaveZip(path)
}

func listCheckpoints(profileID string) ([]Checkpoint, error) {
	directory := checkpointFilesDirectory(profileID)
	entries, err := os.ReadDir(directory)
	if errors.Is(err, os.ErrNotExist) {
		return []Checkpoint{}, nil
	}
	if err != nil {
		return nil, err
	}
	checkpoints := make([]Checkpoint, 0)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".zip") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".zip")
		if !checkpointIDPattern.MatchString(id) {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
			continue
		}
		checkpoint := Checkpoint{ID: id, FileName: entry.Name(), CreatedAt: info.ModTime().UTC(), Trigger: "unknown", Size: info.Size()}
		metadataContents, metadataErr := os.ReadFile(checkpointMetadataPath(profileID, id))
		if metadataErr == nil {
			var metadata checkpointMetadata
			if json.Unmarshal(metadataContents, &metadata) == nil && metadata.ID == id {
				checkpoint.CreatedAt = metadata.CreatedAt
				checkpoint.Trigger = metadata.Trigger
				checkpoint.SourceSave = metadata.SourceSave
			}
		}
		checkpoints = append(checkpoints, checkpoint)
	}
	sort.SliceStable(checkpoints, func(i, j int) bool { return checkpoints[i].CreatedAt.After(checkpoints[j].CreatedAt) })
	return checkpoints, nil
}

func findCheckpoint(profileID, id string) (Checkpoint, string, error) {
	if !checkpointIDPattern.MatchString(id) {
		return Checkpoint{}, "", ErrCheckpointNotFound
	}
	checkpoints, err := listCheckpoints(profileID)
	if err != nil {
		return Checkpoint{}, "", err
	}
	for _, checkpoint := range checkpoints {
		if checkpoint.ID == id {
			return checkpoint, filepath.Join(checkpointFilesDirectory(profileID), checkpoint.FileName), nil
		}
	}
	return Checkpoint{}, "", ErrCheckpointNotFound
}

func enforceCheckpointRetention(profileID string, retention int) error {
	if retention == 0 {
		return nil
	}
	checkpoints, err := listCheckpoints(profileID)
	if err != nil {
		return err
	}
	var cleanupErrors []error
	for _, checkpoint := range checkpoints[min(retention, len(checkpoints)):] {
		if err := os.Remove(filepath.Join(checkpointFilesDirectory(profileID), checkpoint.FileName)); err != nil && !errors.Is(err, os.ErrNotExist) {
			cleanupErrors = append(cleanupErrors, err)
			continue
		}
		if err := os.Remove(checkpointMetadataPath(profileID, checkpoint.ID)); err != nil && !errors.Is(err, os.ErrNotExist) {
			cleanupErrors = append(cleanupErrors, err)
		}
	}
	return errors.Join(cleanupErrors...)
}

func availableRestoredSaveName(id string) (string, error) {
	directory := checkpointSavesPath()
	base := "restored-" + id
	for suffix := 0; suffix < 1000; suffix++ {
		name := base + ".zip"
		if suffix > 0 {
			name = fmt.Sprintf("%s-%d.zip", base, suffix)
		}
		if _, err := os.Stat(filepath.Join(directory, name)); errors.Is(err, os.ErrNotExist) {
			return name, nil
		} else if err != nil {
			return "", err
		}
	}
	return "", errors.New("unable to allocate a restored save name")
}

func recordCheckpointErrorUnlocked(profileID string, checkpointErr error) error {
	store, err := loadCheckpointStore(profileID)
	if err != nil {
		return err
	}
	now := checkpointNow().UTC()
	store.LastError = checkpointErr.Error()
	store.LastErrorAt = &now
	return saveCheckpointStore(profileID, store)
}

// MigrateLegacyProfileCheckpoints moves legacy profile-bound checkpoint stores
// into the default server's directory so persisted checkpoints remain available
// after the architecture change to per-server checkpoint storage. Migration is
// idempotent: running it multiple times does not duplicate or lose data. On
// filename collision (e.g. two legacy stores with checkpoints created at the
// same second), the source file is preserved in its legacy location rather than
// overwritten or discarded. The migration only considers top-level directories
// in the checkpoint root and never touches the servers/ subdirectory that holds
// new per-server stores.
func MigrateLegacyProfileCheckpoints() error {
	root := checkpointRootPath()
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}

	defaultDir := checkpointServerDirectory(DefaultServerID)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		profileID := entry.Name()
		// Skip the per-server subdirectory entirely.
		if profileID == checkpointServersSubdir {
			continue
		}
		// Skip the default server's own store (it is not a legacy profile store).
		if profileID == DefaultServerID {
			continue
		}
		// Verify this is a legacy profile checkpoint store by checking for the
		// files/ subdirectory. Use Lstat to detect symlinks.
		sourceDir := filepath.Join(root, profileID)
		info, err := os.Lstat(sourceDir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("stat legacy checkpoint dir %s: %w", profileID, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			log.Printf("Checkpoint migration: skipping symlinked directory %s", profileID)
			continue
		}
		if !info.IsDir() {
			continue
		}
		sourceFiles := filepath.Join(sourceDir, checkpointFilesDir)
		if _, err := os.Lstat(sourceFiles); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return fmt.Errorf("stat legacy checkpoint files dir %s: %w", profileID, err)
		}

		if err := migrateCheckpointStore(root, profileID, defaultDir); err != nil {
			return fmt.Errorf("migrate legacy checkpoints for %s: %w", profileID, err)
		}
	}
	return nil
}

func migrateCheckpointStore(root, profileID, destinationDir string) error {
	sourceDir := filepath.Join(root, profileID)
	destinationFiles := filepath.Join(destinationDir, checkpointFilesDir)
	if err := os.MkdirAll(destinationFiles, 0700); err != nil {
		return err
	}

	// Migrate the files/ subdirectory. On collision, preserve the source file
	// in its legacy location rather than discarding it. Zip and metadata files
	// are moved as a pair: if either collides, both are preserved.
	sourceFiles := filepath.Join(sourceDir, checkpointFilesDir)
	entries, err := os.ReadDir(sourceFiles)
	if err != nil {
		return err
	}
	// Build a set of destination filenames that already exist before migration.
	// This prevents a pair from colliding with itself during the migration.
	destNames := map[string]bool{}
	if destEntries, err := os.ReadDir(destinationFiles); err == nil {
		for _, destEntry := range destEntries {
			destNames[destEntry.Name()] = true
		}
	}
	for _, entry := range entries {
		sourcePath := filepath.Join(sourceFiles, entry.Name())
		// Validate the entry is not a symlink.
		entryInfo, err := os.Lstat(sourcePath)
		if err != nil {
			return fmt.Errorf("stat source entry %s: %w", entry.Name(), err)
		}
		if entryInfo.Mode()&os.ModeSymlink != 0 {
			log.Printf("Checkpoint migration: skipping symlinked file %s", entry.Name())
			continue
		}
		if !entryInfo.Mode().IsRegular() {
			continue
		}

		// Determine the paired file (zip <-> json metadata).
		var pairName string
		if strings.HasSuffix(entry.Name(), ".zip") {
			pairName = strings.TrimSuffix(entry.Name(), ".zip") + ".json"
		} else if strings.HasSuffix(entry.Name(), ".json") {
			pairName = strings.TrimSuffix(entry.Name(), ".json") + ".zip"
		}

		// Check for collision against the pre-migration destination state.
		destinationPath := filepath.Join(destinationFiles, entry.Name())
		destExists := destNames[entry.Name()]
		var destPairExists bool
		if pairName != "" {
			destPairExists = destNames[pairName]
		}
		if destExists || destPairExists {
			log.Printf("Checkpoint migration: %s (or its pair) already exists in default server store; preserving legacy copy", entry.Name())
			continue
		}
		if err := os.Rename(sourcePath, destinationPath); err != nil {
			return err
		}
	}

	// Migrate the state.json file only if the default server does not already
	// have one, so existing per-server settings are never overwritten.
	sourceState := filepath.Join(sourceDir, checkpointStateFile)
	destinationState := filepath.Join(destinationDir, checkpointStateFile)
	if _, err := os.Lstat(destinationState); errors.Is(err, os.ErrNotExist) {
		if _, err := os.Lstat(sourceState); errors.Is(err, os.ErrNotExist) {
			// No source state to migrate.
		} else if err != nil {
			return fmt.Errorf("stat source state %s: %w", sourceState, err)
		} else {
			if err := os.Rename(sourceState, destinationState); err != nil {
				return err
			}
		}
	}

	return nil
}

const checkpointServersSubdir = "servers"

// checkpointServerStoreID returns the store identifier for a server's
// checkpoint archives. Per-server stores use the "servers/{serverID}"
// identifier to keep them disjoint from legacy profile-bound stores
// which use bare profile IDs.
func checkpointServerStoreID(serverID string) string {
	return filepath.Join(checkpointServersSubdir, serverID)
}

// checkpointServerDirectory returns the storage directory for a server's
// checkpoint archives and state.
func checkpointServerDirectory(serverID string) string {
	return filepath.Join(checkpointRootPath(), checkpointServerStoreID(serverID))
}

func deleteProfileCheckpoints(profileID string) error {
	if err := validateProfileID(profileID); err != nil {
		return err
	}
	// Never delete the per-server stores subdirectory.
	if profileID == checkpointServersSubdir {
		return fmt.Errorf("cannot delete per-server checkpoint namespace")
	}
	return os.RemoveAll(checkpointProfileDirectory(profileID))
}
