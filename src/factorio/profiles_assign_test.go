package factorio

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCreateGlobalPresetExcludesWorldIdentityAndCredentials(t *testing.T) {
	env := setupProfileTest(t)
	require.NoError(t, os.Remove(filepath.Join(env.active["mods"], "current_1.0.0.zip")))
	require.NoError(t, InitializeProfiles())
	require.NoError(t, os.WriteFile(filepath.Join(env.active["mods"], "runtime-cache.json"), []byte(`{"private":"must not enter a preset"}`), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(env.active["config"], "server-settings.json"), []byte(`{"name":"Private game","game_password":"private-password","username":"private-account","max_players":17}`), 0600))
	state, err := CreateGlobalProfile("Safe preset", "")
	require.NoError(t, err)
	preset := findProfileByName(t, state, "Safe preset")
	require.True(t, preset.IsGlobal)
	require.True(t, preset.PresetOnly)
	require.Empty(t, preset.ServerID)
	require.Empty(t, preset.SelectedSave)
	require.Empty(t, preset.BindIP)
	require.Zero(t, preset.Port)
	require.Zero(t, preset.SaveCount)
	require.NoDirExists(t, filepath.Join(profileDirectory(preset.ID), "saves"))
	require.NoDirExists(t, filepath.Join(profileDirectory(preset.ID), "checkpoints"))
	require.NoFileExists(t, filepath.Join(profileDirectory(preset.ID), "config", "config.ini"))
	require.NoFileExists(t, filepath.Join(profileDirectory(preset.ID), "mods", "runtime-cache.json"))
	var settings map[string]interface{}
	require.NoError(t, readBackupJSON(filepath.Join(profileDirectory(preset.ID), "config", "server-settings.json"), &settings))
	require.Equal(t, map[string]interface{}{"max_players": float64(17)}, settings)
	_, err = ActivateProfile(preset.ID)
	require.ErrorIs(t, err, ErrInvalidProfile, "legacy activation must not treat a reusable preset as a world snapshot")
	assertFileContents(t, filepath.Join(env.active["saves"], "current.zip"), "current save")
}

func TestLegacyGlobalSnapshotStillActivatesWithoutPresetConversion(t *testing.T) {
	env := setupProfileTest(t)
	require.NoError(t, InitializeProfiles())
	state, err := CreateProfile("Legacy global snapshot", "", ProfileSourceClone)
	require.NoError(t, err)
	legacy := findProfileByName(t, state, "Legacy global snapshot")
	manifest, err := loadProfileManifest()
	require.NoError(t, err)
	manifest.Profiles[profileIndex(manifest, legacy.ID)].IsGlobal = true
	require.NoError(t, saveProfileManifest(manifest))
	activated, err := ActivateProfile(legacy.ID)
	require.NoError(t, err)
	require.Equal(t, legacy.ID, activated.ActiveProfileID)
	require.False(t, findActiveProfile(t, activated).PresetOnly)
	assertFileContents(t, filepath.Join(env.active["saves"], "current.zip"), "current save")
}

func TestPresetCaptureRecognizesVersionSpecificRecycler(t *testing.T) {
	for _, version := range []Version{{2, 1, 20}, {2, 0, 77}} {
		t.Run(version.ReleaseString(), func(t *testing.T) {
			env := setupProfileTest(t)
			require.NoError(t, os.Remove(filepath.Join(env.active["mods"], "current_1.0.0.zip")))
			require.NoError(t, InitializeProfiles())
			stateMutex.Lock()
			GetFactorioServer().Version = version
			stateMutex.Unlock()
			require.NoError(t, os.WriteFile(filepath.Join(env.active["mods"], "mod-list.json"), []byte(`{"mods":[{"name":"base","enabled":true},{"name":"recycler","enabled":true}]}`), 0600))
			state, err := CreateGlobalProfile("Recycler preset", "")
			if version[1] == 0 {
				require.ErrorIs(t, err, ErrInvalidProfile)
				return
			}
			require.NoError(t, err)
			preset := findProfileByName(t, state, "Recycler preset")
			require.Equal(t, []string{"recycler"}, preset.EnabledBuiltInMods)
			require.Equal(t, GameModeCustom, preset.GameMode)
		})
	}
}

func TestCreateServerPresetCapturesTargetWhileAnotherGameRuns(t *testing.T) {
	env := setupProfileTest(t)
	require.NoError(t, InitializeProfiles())
	_, manager := installTestServerManager(t)
	target, err := manager.CreateServer("Target", "stable", "127.0.0.1", 34201, false)
	require.NoError(t, err)
	target.Version = Version{2, 1, 14}
	target.SavesDir = filepath.Join(env.root, "target", "saves")
	target.ModsDir = filepath.Join(env.root, "target", "mods")
	target.ConfigDir = filepath.Join(env.root, "target", "config")
	target.Savefile = "target.zip"
	for _, directory := range []string{target.SavesDir, target.ModsDir, target.ConfigDir} {
		require.NoError(t, os.MkdirAll(directory, 0700))
	}
	require.NoError(t, os.WriteFile(filepath.Join(target.SavesDir, "target.zip"), []byte("target-owned world"), 0600))
	writeMergeMod(t, target.ModsDir, "target-only", "1.0.0", "base >= 2.1")
	require.NoError(t, os.WriteFile(filepath.Join(target.ModsDir, "mod-list.json"), []byte(`{"mods":[{"name":"base","enabled":true},{"name":"target-only","enabled":true}]}`), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(target.ConfigDir, "server-settings.json"), []byte(`{"name":"Target game","max_players":23,"game_password":"target-private"}`), 0600))
	other := GetFactorioServer()
	stateMutex.Lock()
	other.Running = true
	stateMutex.Unlock()
	t.Cleanup(func() { stateMutex.Lock(); other.Running = false; stateMutex.Unlock() })
	state, err := CreateServerProfile(target.ID, "Target preset", "")
	require.NoError(t, err)
	preset := findProfileByName(t, state, "Target preset")
	require.True(t, preset.IsGlobal, "captured presets must be reusable rather than owned by one game")
	require.True(t, preset.PresetOnly)
	require.Empty(t, preset.ServerID)
	require.Zero(t, preset.SaveCount)
	require.Empty(t, preset.SelectedSave)
	require.FileExists(t, filepath.Join(profileDirectory(preset.ID), "mods", "target-only_1.0.0.zip"))
	require.NoFileExists(t, filepath.Join(profileDirectory(preset.ID), "mods", "current_1.0.0.zip"))
	require.NoDirExists(t, filepath.Join(profileDirectory(preset.ID), "saves"))
	var settings map[string]interface{}
	require.NoError(t, readBackupJSON(filepath.Join(profileDirectory(preset.ID), "config", "server-settings.json"), &settings))
	require.Equal(t, map[string]interface{}{"max_players": float64(23)}, settings)
	require.True(t, other.GetRunning())
	require.Equal(t, "target.zip", target.Savefile)
	assertFileContents(t, filepath.Join(target.SavesDir, "target.zip"), "target-owned world")
}

func TestPresetCreationManifestFailurePreservesExistingProfiles(t *testing.T) {
	env := setupProfileTest(t)
	require.NoError(t, os.Remove(filepath.Join(env.active["mods"], "current_1.0.0.zip")))
	require.NoError(t, InitializeProfiles())
	manifestPath := filepath.Join(profileRootPath(), profileManifestName)
	before, err := os.ReadFile(manifestPath)
	require.NoError(t, err)
	entriesBefore, err := os.ReadDir(profileRootPath())
	require.NoError(t, err)
	original := atomicFileRename
	t.Cleanup(func() { atomicFileRename = original })
	atomicFileRename = func(source, destination string) error {
		if destination == manifestPath && strings.HasPrefix(filepath.Base(source), ".fsm-write-") {
			return errors.New("synthetic preset manifest activation failure")
		}
		return os.Rename(source, destination)
	}
	_, err = CreateGlobalProfile("Must not publish", "")
	require.Error(t, err)
	after, err := os.ReadFile(manifestPath)
	require.NoError(t, err)
	require.Equal(t, before, after)
	var manifest profileManifest
	require.NoError(t, json.Unmarshal(after, &manifest))
	require.Len(t, manifest.Profiles, 1)
	entriesAfter, err := os.ReadDir(profileRootPath())
	require.NoError(t, err)
	require.Len(t, entriesAfter, len(entriesBefore))
}

func setupPresetApplication(t *testing.T) (*profileTestEnvironment, Profile, *ServerManager, *Server) {
	t.Helper()
	env := setupProfileTest(t)
	require.NoError(t, os.Remove(filepath.Join(env.active["mods"], "current_1.0.0.zip")))
	require.NoError(t, InitializeProfiles())
	state, err := CreateGlobalProfile("Reusable settings", "")
	require.NoError(t, err)
	preset := findProfileByName(t, state, "Reusable settings")
	id, manager := installTestServerManager(t)
	server, ok := manager.GetServer(id)
	require.True(t, ok)
	server.Version = Version{2, 1, 14}
	server.BaseModVersion = "2.1.14"
	server.Savefile = "owned.zip"
	server.Settings = map[string]interface{}{"name": "Target game", "game_password": "target-secret", "max_players": float64(5)}
	for _, dir := range []string{server.SavesDir, server.ModsDir, server.ConfigDir} {
		require.NoError(t, os.MkdirAll(dir, 0700))
	}
	require.NoError(t, os.WriteFile(filepath.Join(server.SavesDir, "owned.zip"), []byte("target world"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(server.ModsDir, "previous.txt"), []byte("old mod state"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(server.ConfigDir, "server-settings.json"), []byte(`{"name":"Target game","game_password":"target-secret","max_players":5}`), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(server.ConfigDir, "config.ini"), []byte("target local paths"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(profileDirectory(preset.ID), "config", "server-settings.json"), []byte(`{"name":"Source game","game_password":"source-secret","username":"source-account","token":"source-token","admins":["source-admin"],"max_players":20}`), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(profileDirectory(preset.ID), "config", "config.ini"), []byte("source private paths"), 0600))
	require.NoError(t, manager.UpdateServer(server))
	return env, preset, manager, server
}

func TestPresetAssignmentPreservesCredentialsAndSource(t *testing.T) {
	env, preset, manager, first := setupPresetApplication(t)
	manifestPath := filepath.Join(profileRootPath(), profileManifestName)
	manifestBefore, err := os.ReadFile(manifestPath)
	require.NoError(t, err)
	modsBefore, err := os.Stat(first.ModsDir)
	require.NoError(t, err)
	configBefore, err := os.Stat(first.ConfigDir)
	require.NoError(t, err)
	require.NoError(t, AssignProfileToServer(first.ID, preset.ID))
	assertFileContents(t, filepath.Join(first.SavesDir, "owned.zip"), "target world")
	assertFileContents(t, filepath.Join(first.ConfigDir, "config.ini"), "target local paths")
	settings, err := os.ReadFile(filepath.Join(first.ConfigDir, "server-settings.json"))
	require.NoError(t, err)
	require.NotContains(t, string(settings), "source-")
	require.Contains(t, string(settings), "target-secret")
	require.Equal(t, float64(20), first.SettingsSnapshot()["max_players"])
	require.NoFileExists(t, filepath.Join(first.ModsDir, "previous.txt"))
	modsAfter, err := os.Stat(first.ModsDir)
	require.NoError(t, err)
	configAfter, err := os.Stat(first.ConfigDir)
	require.NoError(t, err)
	require.True(t, os.SameFile(modsBefore, modsAfter), "mods mount point was replaced")
	require.True(t, os.SameFile(configBefore, configAfter), "config mount point was replaced")

	second, err := manager.CreateServer("Second", "stable", "127.0.0.1", 34201, false)
	require.NoError(t, err)
	second.SavesDir = filepath.Join(env.root, "second", "saves")
	second.ModsDir = filepath.Join(env.root, "second", "mods")
	second.ConfigDir = filepath.Join(env.root, "second", "config")
	second.Version = first.Version
	second.Settings = map[string]interface{}{"name": "Second game"}
	// A running unrelated server must not be stopped or mutated by assignment.
	stateMutex.Lock()
	first.Running = true
	stateMutex.Unlock()
	t.Cleanup(func() { stateMutex.Lock(); first.Running = false; stateMutex.Unlock() })
	require.NoError(t, AssignProfileToServer(second.ID, preset.ID))
	require.True(t, first.GetRunning())
	require.Equal(t, preset.ID, first.ProfileID)
	require.Equal(t, preset.ID, second.ProfileID)
	listed, err := ListProfilesForServer(second.ID)
	require.NoError(t, err)
	require.Equal(t, preset.ID, listed.ActiveProfileID)
	require.True(t, findProfileByName(t, listed, preset.Name).Active)
	manifestAfter, err := os.ReadFile(manifestPath)
	require.NoError(t, err)
	require.Equal(t, manifestBefore, manifestAfter, "application changed source profile metadata")
	require.NoError(t, os.WriteFile(filepath.Join(first.ModsDir, "mod-settings.dat"), []byte("first changed"), 0600))
	assertFileContents(t, filepath.Join(second.ModsDir, "mod-settings.dat"), "current profile mod settings")
	assertFileContents(t, filepath.Join(profileDirectory(preset.ID), "mods", "mod-settings.dat"), "current profile mod settings")
}

func TestPresetAssignmentRollsBackActivationAndCatalogFailures(t *testing.T) {
	for _, failure := range []string{"config activation", "catalog persistence", "partial mods backup"} {
		t.Run(failure, func(t *testing.T) {
			_, preset, manager, server := setupPresetApplication(t)
			before, err := os.ReadFile(manager.catalogFile)
			require.NoError(t, err)
			originalRename := profileRename
			originalAtomicRename := atomicFileRename
			t.Cleanup(func() { profileRename = originalRename; atomicFileRename = originalAtomicRename })
			if failure == "partial mods backup" {
				require.NoError(t, os.WriteFile(filepath.Join(server.ModsDir, "zz-previous.txt"), []byte("second original file"), 0600))
				profileRename = func(source, destination string) error {
					if source == filepath.Join(server.ModsDir, "zz-previous.txt") && strings.HasPrefix(filepath.Base(filepath.Dir(destination)), ".profile-backup-") {
						return errors.New("synthetic partial backup failure")
					}
					return os.Rename(source, destination)
				}
			} else if failure == "config activation" {
				profileRename = func(source, destination string) error {
					if destination == filepath.Join(server.ConfigDir, "server-settings.json") && strings.Contains(source, ".profile-staging-") {
						return errors.New("synthetic preset activation failure")
					}
					return os.Rename(source, destination)
				}
			} else {
				atomicFileRename = func(source, destination string) error {
					if destination == manager.catalogFile && strings.HasPrefix(filepath.Base(source), ".fsm-write-") {
						return errors.New("synthetic preset catalog failure")
					}
					return os.Rename(source, destination)
				}
			}
			require.Error(t, AssignProfileToServer(server.ID, preset.ID))
			assertFileContents(t, filepath.Join(server.ModsDir, "previous.txt"), "old mod state")
			if failure == "partial mods backup" {
				assertFileContents(t, filepath.Join(server.ModsDir, "zz-previous.txt"), "second original file")
			}
			assertFileContents(t, filepath.Join(server.SavesDir, "owned.zip"), "target world")
			assertFileContents(t, filepath.Join(server.ConfigDir, "server-settings.json"), `{"name":"Target game","game_password":"target-secret","max_players":5}`)
			require.Empty(t, server.ProfileID)
			require.Equal(t, float64(5), server.SettingsSnapshot()["max_players"])
			after, err := os.ReadFile(manager.catalogFile)
			require.NoError(t, err)
			require.Equal(t, before, after)
		})
	}
}

func TestPresetAssignmentRejectsInvalidInputWithoutMutation(t *testing.T) {
	for _, invalid := range []string{"unknown version", "incompatible version", "invalid mods", "invalid setting", "feature mods", "running target", "missing dependency"} {
		t.Run(invalid, func(t *testing.T) {
			_, preset, manager, server := setupPresetApplication(t)
			before, err := os.ReadFile(manager.catalogFile)
			require.NoError(t, err)
			switch invalid {
			case "unknown version":
				server.Version = NilVersion
			case "incompatible version":
				server.Version = Version{1, 1, 110}
			case "invalid mods":
				require.NoError(t, os.WriteFile(filepath.Join(profileDirectory(preset.ID), "mods", "bad_1.0.0.zip"), []byte("not a zip"), 0600))
			case "invalid setting":
				require.NoError(t, os.WriteFile(filepath.Join(profileDirectory(preset.ID), "config", "server-settings.json"), []byte(`{"max_players":"not a number"}`), 0600))
			case "feature mods":
				require.NoError(t, os.WriteFile(filepath.Join(profileDirectory(preset.ID), "mods", "mod-list.json"), []byte(`{"mods":[{"name":"base","enabled":true},{"name":"quality","enabled":true}]}`), 0600))
			case "running target":
				server.Running = true
			case "missing dependency":
				mods := filepath.Join(profileDirectory(preset.ID), "mods")
				writeMergeMod(t, mods, "needs-other", "1.0.0", "base >= 2.1", "missing-mod >= 1.0")
				require.NoError(t, os.WriteFile(filepath.Join(mods, "mod-list.json"), []byte(`{"mods":[{"name":"base","enabled":true},{"name":"needs-other","enabled":true}]}`), 0600))
			}
			require.Error(t, AssignProfileToServer(server.ID, preset.ID))
			assertFileContents(t, filepath.Join(server.ModsDir, "previous.txt"), "old mod state")
			assertFileContents(t, filepath.Join(server.SavesDir, "owned.zip"), "target world")
			assertFileContents(t, filepath.Join(server.ConfigDir, "server-settings.json"), `{"name":"Target game","game_password":"target-secret","max_players":5}`)
			after, err := os.ReadFile(manager.catalogFile)
			require.NoError(t, err)
			require.Equal(t, before, after)
			require.Empty(t, server.ProfileID)
		})
	}
}

func TestPresetAssignmentAtMountedDestination(t *testing.T) {
	modsDir, configDir := os.Getenv("FSM_TEST_MOUNTED_MODS_DIR"), os.Getenv("FSM_TEST_MOUNTED_CONFIG_DIR")
	if modsDir == "" || configDir == "" {
		t.Skip("requires disposable mounted mods and config directories")
	}
	_, preset, _, server := setupPresetApplication(t)
	for _, dir := range []string{modsDir, configDir} {
		entries, err := os.ReadDir(dir)
		require.NoError(t, err)
		for _, entry := range entries {
			require.NoError(t, os.RemoveAll(filepath.Join(dir, entry.Name())))
		}
	}
	server.ModsDir, server.ConfigDir = modsDir, configDir
	modsMount, err := os.Stat(modsDir)
	require.NoError(t, err)
	configMount, err := os.Stat(configDir)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(modsDir, "previous.txt"), []byte("mounted mods"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "server-settings.json"), []byte(`{"name":"Mounted game","game_password":"local-only"}`), 0600))
	originalRename := profileRename
	profileRename = func(source, destination string) error {
		if destination == filepath.Join(configDir, "server-settings.json") && strings.Contains(source, ".profile-staging-") {
			return errors.New("synthetic mounted activation failure")
		}
		return os.Rename(source, destination)
	}
	t.Cleanup(func() { profileRename = originalRename })
	require.Error(t, AssignProfileToServer(server.ID, preset.ID))
	assertFileContents(t, filepath.Join(modsDir, "previous.txt"), "mounted mods")
	assertFileContents(t, filepath.Join(configDir, "server-settings.json"), `{"name":"Mounted game","game_password":"local-only"}`)
	profileRename = originalRename
	require.NoError(t, AssignProfileToServer(server.ID, preset.ID))
	modsAfter, err := os.Stat(modsDir)
	require.NoError(t, err)
	configAfter, err := os.Stat(configDir)
	require.NoError(t, err)
	require.True(t, os.SameFile(modsMount, modsAfter))
	require.True(t, os.SameFile(configMount, configAfter))
	require.NoFileExists(t, filepath.Join(modsDir, "previous.txt"))
	require.Equal(t, "local-only", server.SettingsSnapshot()["game_password"])
}

// setupAssignServerManager builds a ServerManager whose server points its
// storage paths at a temporary directory so profile assignment exercises only
// isolated scratch space.
func setupAssignServerManager(t *testing.T) (string, *ServerManager) {
	t.Helper()
	root := t.TempDir()
	manager := &ServerManager{
		catalogFile: filepath.Join(root, "servers.json"),
		servers:     make(map[string]*Server),
	}
	server, err := manager.CreateServer("Server 1", "stable", "127.0.0.1", 34200, false)
	require.NoError(t, err)
	server.SavesDir = filepath.Join(root, "servers", server.ID, "saves")
	server.ModsDir = filepath.Join(root, "servers", server.ID, "mods")
	server.ConfigDir = filepath.Join(root, "servers", server.ID, "config")
	return server.ID, manager
}

// installTestServerManager swaps the package-global server manager for a
// temporary one and restores the previous value when the test completes.
func installTestServerManager(t *testing.T) (string, *ServerManager) {
	t.Helper()
	previous := serverManager
	serverID, manager := setupAssignServerManager(t)
	serverManager = manager
	t.Cleanup(func() { serverManager = previous })
	return serverID, manager
}

// The public application path must neither deadlock nor create another template.
func TestApplyGlobalProfileToServerKeepsReusableSource(t *testing.T) {
	environment := setupProfileTest(t)
	require.NoError(t, os.Remove(filepath.Join(environment.active["mods"], "current_1.0.0.zip")))
	require.NoError(t, InitializeProfiles())

	// Build a global template from the active setup before any manager exists.
	globalState, err := CreateGlobalProfile("Modded template", "Base template")
	require.NoError(t, err)
	global := findProfileByName(t, globalState, "Modded template")
	require.True(t, global.IsGlobal)

	serverID, manager := installTestServerManager(t)
	server, ok := manager.GetServer(serverID)
	require.True(t, ok)
	server.Version = Version{2, 1, 14}
	require.NoError(t, os.MkdirAll(server.SavesDir, 0700))
	server.Savefile = "target-world.zip"
	world := []byte("distinct target-owned saved world")
	require.NoError(t, os.WriteFile(filepath.Join(server.SavesDir, server.Savefile), world, 0600))
	name, bindIP, port := server.Name, server.BindIP, server.Port
	rconPort, rconPass := server.RconPort, server.RconPass

	_, err = ApplyGlobalProfileToServer(serverID, global.ID)
	require.NoError(t, err)

	// The target references the reusable source and keeps its own identity.
	server, ok = manager.GetServer(serverID)
	require.True(t, ok)
	require.NotEmpty(t, server.ProfileID)
	require.Equal(t, global.ID, server.ProfileID)
	require.Equal(t, name, server.Name)
	require.Equal(t, bindIP, server.BindIP)
	require.Equal(t, port, server.Port)
	require.Equal(t, rconPort, server.RconPort)
	require.Equal(t, rconPass, server.RconPass)
	require.Equal(t, "target-world.zip", server.Savefile)
	actualWorld, err := os.ReadFile(filepath.Join(server.SavesDir, server.Savefile))
	require.NoError(t, err)
	require.Equal(t, world, actualWorld)

	// Applying a template does not transfer ownership or add a profile record.
	state, err := ListProfiles()
	require.NoError(t, err)
	require.Len(t, state.Profiles, 2)
	found := false
	for _, profile := range state.Profiles {
		if profile.ID == server.ProfileID {
			require.Empty(t, profile.ServerID)
			found = true
			break
		}
	}
	require.True(t, found, "applied profile was not recorded")

	// Only mods and config are copied onto the server; saves are untouched.
	require.FileExists(t, filepath.Join(server.ModsDir, "mod-list.json"))
	require.FileExists(t, filepath.Join(server.ConfigDir, "server-settings.json"))
	entries, err := os.ReadDir(server.SavesDir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, server.Savefile, entries[0].Name())

	// The global template and its data are left intact.
	_, statErr := os.Stat(filepath.Join(environment.root, "profiles", global.ID, "mods", "mod-list.json"))
	require.NoError(t, statErr)
}

// TestAssignProfileToServerFailsClosedWithoutManager verifies that a missing
// server manager is refused rather than silently falling back.
func TestAssignProfileToServerFailsClosedWithoutManager(t *testing.T) {
	setupProfileTest(t)
	require.NoError(t, InitializeProfiles())
	state, err := ListProfiles()
	require.NoError(t, err)
	active := findActiveProfile(t, state)

	require.Empty(t, active.ServerID)
	require.True(t, active.Active)

	err = AssignProfileToServer(DefaultServerID, active.ID)
	require.ErrorIs(t, err, ErrServerMissing)

	// Mutations are withheld: the profile's binding and selection are unchanged.
	reloaded, err := ListProfiles()
	require.NoError(t, err)
	after := findProfileByName(t, reloaded, active.Name)
	require.Equal(t, active.ID, after.ID)
	require.Empty(t, after.ServerID)
	require.True(t, after.Active)
}

// TestAssignProfileToServerFailsClosedForUnknownServer verifies that an unknown
// server id is refused rather than silently falling back to the default.
func TestAssignProfileToServerFailsClosedForUnknownServer(t *testing.T) {
	setupProfileTest(t)
	require.NoError(t, InitializeProfiles())
	state, err := ListProfiles()
	require.NoError(t, err)
	active := findActiveProfile(t, state)

	_, manager := installTestServerManager(t)
	_, known := manager.GetServer("2")
	require.False(t, known)

	err = AssignProfileToServer("2", active.ID)
	require.ErrorIs(t, err, ErrServerMissing)

	// Mutations are withheld: the profile's binding and selection are unchanged.
	reloaded, err := ListProfiles()
	require.NoError(t, err)
	after := findProfileByName(t, reloaded, active.Name)
	require.Equal(t, active.ID, after.ID)
	require.Empty(t, after.ServerID)
	require.True(t, after.Active)
}

func TestApplyGlobalProfileToMultipleServersKeepsReusableSource(t *testing.T) {
	environment := setupProfileTest(t)
	require.NoError(t, os.Remove(filepath.Join(environment.active["mods"], "current_1.0.0.zip")))
	require.NoError(t, InitializeProfiles())

	globalState, err := CreateGlobalProfile("Multi-target template", "")
	require.NoError(t, err)
	global := findProfileByName(t, globalState, "Multi-target template")
	require.True(t, global.IsGlobal)

	// Create a manager with two servers
	serverID1, manager := installTestServerManager(t)
	server1, ok := manager.GetServer(serverID1)
	require.True(t, ok)
	server1.Version = Version{2, 1, 14}
	require.NoError(t, os.MkdirAll(server1.SavesDir, 0700))
	server1.Savefile = "world-one.zip"
	require.NoError(t, os.WriteFile(filepath.Join(server1.SavesDir, server1.Savefile), []byte("world one"), 0600))

	server2, err := manager.CreateServer("Server 2", "stable", "127.0.0.1", 34201, false)
	require.NoError(t, err)
	server2.SavesDir = filepath.Join(environment.root, "server2-saves")
	server2.ModsDir = filepath.Join(environment.root, "server2-mods")
	server2.ConfigDir = filepath.Join(environment.root, "server2-config")
	server2.Version = Version{2, 1, 14}
	require.NoError(t, os.MkdirAll(server2.SavesDir, 0700))
	server2.Savefile = "world-two.zip"
	require.NoError(t, os.WriteFile(filepath.Join(server2.SavesDir, server2.Savefile), []byte("world two"), 0600))

	// Apply preset to first server
	_, err = ApplyGlobalProfileToServer(serverID1, global.ID)
	require.NoError(t, err)

	// Apply preset to second server
	serverID2 := server2.ID
	_, err = ApplyGlobalProfileToServer(serverID2, global.ID)
	require.NoError(t, err)

	// Both servers reference the same preset
	server1, _ = manager.GetServer(serverID1)
	server2, _ = manager.GetServer(serverID2)
	require.Equal(t, global.ID, server1.ProfileID)
	require.Equal(t, global.ID, server2.ProfileID)

	// Both servers' saves are preserved
	world1, err := os.ReadFile(filepath.Join(server1.SavesDir, server1.Savefile))
	require.NoError(t, err)
	require.Equal(t, []byte("world one"), world1)
	world2, err := os.ReadFile(filepath.Join(server2.SavesDir, server2.Savefile))
	require.NoError(t, err)
	require.Equal(t, []byte("world two"), world2)

	// The preset source is still reusable (not consumed)
	state, err := ListProfiles()
	require.NoError(t, err)
	preset := findProfileByName(t, state, "Multi-target template")
	require.True(t, preset.IsGlobal)
	require.Empty(t, preset.ServerID)
}

func TestApplyGlobalProfileToRunningServerIsBlocked(t *testing.T) {
	environment := setupProfileTest(t)
	require.NoError(t, os.Remove(filepath.Join(environment.active["mods"], "current_1.0.0.zip")))
	require.NoError(t, InitializeProfiles())

	globalState, err := CreateGlobalProfile("Running block template", "")
	require.NoError(t, err)
	global := findProfileByName(t, globalState, "Running block template")
	require.True(t, global.IsGlobal)

	serverID, manager := installTestServerManager(t)
	server, ok := manager.GetServer(serverID)
	require.True(t, ok)
	server.Version = Version{2, 1, 14}
	require.NoError(t, os.MkdirAll(server.SavesDir, 0700))
	server.Savefile = "running-world.zip"
	require.NoError(t, os.WriteFile(filepath.Join(server.SavesDir, server.Savefile), []byte("running world"), 0600))

	// Simulate a running server
	server.Running = true

	_, err = ApplyGlobalProfileToServer(serverID, global.ID)
	require.Error(t, err, "applying a preset to a running server must be blocked")

	// The server's save must be preserved
	actualWorld, err := os.ReadFile(filepath.Join(server.SavesDir, server.Savefile))
	require.NoError(t, err)
	require.Equal(t, []byte("running world"), actualWorld)
}

// TestApplyGlobalProfileToServerFailsClosedWithoutManager verifies that the
// apply path fails closed when no server manager is available.
func TestApplyGlobalProfileToServerFailsClosedWithoutManager(t *testing.T) {
	env := setupProfileTest(t)
	require.NoError(t, os.Remove(filepath.Join(env.active["mods"], "current_1.0.0.zip")))
	require.NoError(t, InitializeProfiles())
	globalState, err := CreateGlobalProfile("Modded template", "Base template")
	require.NoError(t, err)
	global := findProfileByName(t, globalState, "Modded template")

	_, err = ApplyGlobalProfileToServer(DefaultServerID, global.ID)
	require.ErrorIs(t, err, ErrServerMissing)
}

func TestValidateTargetFeatureAvailabilityAcceptsAvailableFeatures(t *testing.T) {
	factorioDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(factorioDir, "data", "quality"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(factorioDir, "data", "quality", "info.json"), []byte(`{"name":"quality"}`), 0600))
	require.NoError(t, os.MkdirAll(filepath.Join(factorioDir, "data", "elevated-rails"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(factorioDir, "data", "elevated-rails", "info.json"), []byte(`{"name":"elevated-rails"}`), 0600))

	original := presetFactorioDir
	presetFactorioDir = func() string { return factorioDir }
	t.Cleanup(func() { presetFactorioDir = original })

	err := validateTargetFeatureAvailability([]string{"quality", "elevated-rails"})
	require.NoError(t, err)
}

func TestValidateTargetFeatureAvailabilityRejectsMissingFeatures(t *testing.T) {
	factorioDir := t.TempDir()
	// Only quality is available, elevated-rails is missing
	require.NoError(t, os.MkdirAll(filepath.Join(factorioDir, "data", "quality"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(factorioDir, "data", "quality", "info.json"), []byte(`{"name":"quality"}`), 0600))

	original := presetFactorioDir
	presetFactorioDir = func() string { return factorioDir }
	t.Cleanup(func() { presetFactorioDir = original })

	err := validateTargetFeatureAvailability([]string{"quality", "elevated-rails"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "elevated-rails")
	require.NotContains(t, err.Error(), "quality")
}

func TestValidateTargetFeatureAvailabilityRejectsInvalidNames(t *testing.T) {
	factorioDir := t.TempDir()
	original := presetFactorioDir
	presetFactorioDir = func() string { return factorioDir }
	t.Cleanup(func() { presetFactorioDir = original })

	err := validateTargetFeatureAvailability([]string{"../escape"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid built-in mod name")
}

func TestValidateTargetFeatureAvailabilityRejectsEmptyDirectory(t *testing.T) {
	original := presetFactorioDir
	presetFactorioDir = func() string { return "" }
	t.Cleanup(func() { presetFactorioDir = original })

	err := validateTargetFeatureAvailability([]string{"quality"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not configured")
}

func TestApplyGlobalProfilePreservesTargetIdentityAndNetwork(t *testing.T) {
	env := setupProfileTest(t)
	require.NoError(t, os.Remove(filepath.Join(env.active["mods"], "current_1.0.0.zip")))
	require.NoError(t, InitializeProfiles())

	// Create a global preset before installing the manager
	globalState, err := CreateGlobalProfile("Identity test preset", "")
	require.NoError(t, err)
	global := findProfileByName(t, globalState, "Identity test preset")
	require.True(t, global.IsGlobal)

	// Install manager and get the target server
	serverID, manager := installTestServerManager(t)
	target, ok := manager.GetServer(serverID)
	require.True(t, ok)
	target.Version = Version{2, 1, 14}
	target.SavesDir = filepath.Join(env.root, "target-saves")
	target.ModsDir = filepath.Join(env.root, "target-mods")
	target.ConfigDir = filepath.Join(env.root, "target-config")
	require.NoError(t, os.MkdirAll(target.SavesDir, 0700))
	require.NoError(t, os.MkdirAll(target.ModsDir, 0700))
	require.NoError(t, os.MkdirAll(target.ConfigDir, 0700))
	target.Savefile = "target.zip"
	require.NoError(t, os.WriteFile(filepath.Join(target.SavesDir, target.Savefile), []byte("target world"), 0600))
	// Target has its own name, bindip, port, and RCON password
	require.NoError(t, os.WriteFile(filepath.Join(target.ConfigDir, "server-settings.json"), []byte(`{"name":"Target Game","bind_address":"192.168.1.100","server_port":34301,"rcon_password":"target-rcon-secret","max_players":10}`), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(target.ModsDir, "mod-list.json"), []byte(`{"mods":[{"name":"base","enabled":true},{"name":"core","enabled":true}]}`), 0600))

	// Apply preset to target
	_, err = ApplyGlobalProfileToServer(target.ID, global.ID)
	require.NoError(t, err)

	// Verify target's identity and network settings are preserved
	var targetSettings map[string]interface{}
	require.NoError(t, readBackupJSON(filepath.Join(target.ConfigDir, "server-settings.json"), &targetSettings))
	require.Equal(t, "Target Game", targetSettings["name"])
	require.Equal(t, "192.168.1.100", targetSettings["bind_address"])
	require.Equal(t, float64(34301), targetSettings["server_port"])
	require.Equal(t, "target-rcon-secret", targetSettings["rcon_password"])

	// Verify target's world is preserved
	assertFileContents(t, filepath.Join(target.SavesDir, target.Savefile), "target world")
}

func TestApplyGlobalProfilePerformsRealConfigAndModChanges(t *testing.T) {
	env := setupProfileTest(t)
	require.NoError(t, os.Remove(filepath.Join(env.active["mods"], "current_1.0.0.zip")))
	require.NoError(t, InitializeProfiles())

	// Create a preset with specific settings
	require.NoError(t, os.WriteFile(filepath.Join(env.active["config"], "server-settings.json"), []byte(`{"name":"Preset Game","max_players":42,"autosave_interval":30}`), 0600))
	globalState, err := CreateGlobalProfile("Config test preset", "")
	require.NoError(t, err)
	global := findProfileByName(t, globalState, "Config test preset")
	require.True(t, global.IsGlobal)

	// Install manager and get the target server
	serverID, manager := installTestServerManager(t)
	target, ok := manager.GetServer(serverID)
	require.True(t, ok)
	target.Version = Version{2, 1, 14}
	target.SavesDir = filepath.Join(env.root, "target-saves")
	target.ModsDir = filepath.Join(env.root, "target-mods")
	target.ConfigDir = filepath.Join(env.root, "target-config")
	require.NoError(t, os.MkdirAll(target.SavesDir, 0700))
	require.NoError(t, os.MkdirAll(target.ModsDir, 0700))
	require.NoError(t, os.MkdirAll(target.ConfigDir, 0700))
	target.Savefile = "target.zip"
	require.NoError(t, os.WriteFile(filepath.Join(target.SavesDir, target.Savefile), []byte("target world"), 0600))
	// Target has different settings before preset application
	require.NoError(t, os.WriteFile(filepath.Join(target.ConfigDir, "server-settings.json"), []byte(`{"name":"Target Game","max_players":5,"autosave_interval":10}`), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(target.ModsDir, "mod-list.json"), []byte(`{"mods":[{"name":"base","enabled":true},{"name":"core","enabled":true}]}`), 0600))

	// Apply preset to target
	_, err = ApplyGlobalProfileToServer(target.ID, global.ID)
	require.NoError(t, err)

	// Verify target's config was actually changed by the preset
	var targetSettings map[string]interface{}
	require.NoError(t, readBackupJSON(filepath.Join(target.ConfigDir, "server-settings.json"), &targetSettings))
	// max_players should now be 42 (from preset), not 5 (original)
	require.Equal(t, float64(42), targetSettings["max_players"])
	// autosave_interval should now be 30 (from preset), not 10 (original)
	require.Equal(t, float64(30), targetSettings["autosave_interval"])
}

func TestApplyGlobalProfileRollbackOnModValidationFailure(t *testing.T) {
	env := setupProfileTest(t)
	require.NoError(t, os.Remove(filepath.Join(env.active["mods"], "current_1.0.0.zip")))
	require.NoError(t, InitializeProfiles())

	// Create a preset with an incompatible mod by manually constructing the preset directory
	// (CreateGlobalProfile validates mods, so we bypass that to create an intentionally incompatible preset)
	presetID := "incompatible-preset"
	presetRoot := filepath.Join(env.root, "profiles", presetID)
	require.NoError(t, os.MkdirAll(filepath.Join(presetRoot, "mods"), 0700))
	require.NoError(t, os.MkdirAll(filepath.Join(presetRoot, "config"), 0700))
	// Write an incompatible mod (requires 2.2, target is 2.1.14)
	writeMergeMod(t, presetRoot, "future-mod", "1.0.0", "base >= 2.2")
	require.NoError(t, os.WriteFile(filepath.Join(presetRoot, "mods", "mod-list.json"), []byte(`{"mods":[{"name":"base","enabled":true},{"name":"core","enabled":true},{"name":"future-mod","enabled":true}]}`), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(presetRoot, "config", "server-settings.json"), []byte(`{"max_players":42}`), 0600))
	// Register the preset in the manifest
	manifest, err := loadProfileManifest()
	require.NoError(t, err)
	manifest.Profiles = append(manifest.Profiles, Profile{
		ID:             presetID,
		Name:           "Incompatible mod preset",
		IsGlobal:       true,
		PresetOnly:     true,
		InstalledVersion: "2.1.14",
		GameMode:       "factorio",
	})
	require.NoError(t, saveProfileManifest(manifest))

	// Install manager and get the target server (running 2.1.14, which is < 2.2)
	serverID, manager := installTestServerManager(t)
	target, ok := manager.GetServer(serverID)
	require.True(t, ok)
	target.Version = Version{2, 1, 14}
	target.SavesDir = filepath.Join(env.root, "target-saves")
	target.ModsDir = filepath.Join(env.root, "target-mods")
	target.ConfigDir = filepath.Join(env.root, "target-config")
	require.NoError(t, os.MkdirAll(target.SavesDir, 0700))
	require.NoError(t, os.MkdirAll(target.ModsDir, 0700))
	require.NoError(t, os.MkdirAll(target.ConfigDir, 0700))
	target.Savefile = "target.zip"
	require.NoError(t, os.WriteFile(filepath.Join(target.SavesDir, target.Savefile), []byte("target world"), 0600))
	// Target has original settings
	require.NoError(t, os.WriteFile(filepath.Join(target.ConfigDir, "server-settings.json"), []byte(`{"name":"Target Game","max_players":5}`), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(target.ModsDir, "mod-list.json"), []byte(`{"mods":[{"name":"base","enabled":true},{"name":"core","enabled":true}]}`), 0600))

	// Apply preset - should fail because future-mod requires 2.2 but target is 2.1.14
	_, err = ApplyGlobalProfileToServer(target.ID, presetID)
	require.Error(t, err, "applying a preset with incompatible mods should fail")

	// Verify target's config was NOT changed (rollback worked)
	var targetSettings map[string]interface{}
	require.NoError(t, readBackupJSON(filepath.Join(target.ConfigDir, "server-settings.json"), &targetSettings))
	require.Equal(t, float64(5), targetSettings["max_players"])

	// Verify target's mods were NOT changed (rollback worked)
	modListContent, err := os.ReadFile(filepath.Join(target.ModsDir, "mod-list.json"))
	require.NoError(t, err)
	require.NotContains(t, string(modListContent), "future-mod")

	// Verify target's world is still intact
	assertFileContents(t, filepath.Join(target.SavesDir, target.Savefile), "target world")
}
