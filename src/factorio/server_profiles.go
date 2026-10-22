package factorio

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// Presets deliberately exclude identity, credentials, visibility, access lists,
// paths and networking. Unknown settings stay local until explicitly supported.
var presetSettings = map[string]string{
	"max_players": "number", "ignore_player_limit_for_returning_players": "bool",
	"autosave_interval": "number", "autosave_slots": "number", "autosave_only_on_server": "bool",
	"non_blocking_saving": "bool", "afk_autokick_interval": "number", "auto_pause": "bool",
	"auto_pause_when_players_connect": "bool", "only_admins_can_pause_the_game": "bool",
	"allow_commands": "string",
}

func presetModExclusions(root string) (map[string]bool, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	excluded := make(map[string]bool)
	for _, entry := range entries {
		// Runtime caches, portal metadata and temporary files are not presets.
		// Enabled unpacked mods will fail validation rather than being copied.
		if !backupModFile(entry.Name()) {
			excluded[entry.Name()] = true
		}
	}
	return excluded, nil
}

func presetAdditionalBuiltIns(version Version) []string {
	// Factorio 2.1 split recycler into its own official data mod. Recognizing
	// it here is not permission to enable it on an unvalidated target engine.
	if version[0] == 2 && version[1] >= 1 {
		return []string{"recycler"}
	}
	return nil
}

func presetSettingValues(source map[string]interface{}) (map[string]interface{}, error) {
	settings := make(map[string]interface{})
	for key, kind := range presetSettings {
		value, present := source[key]
		if !present {
			continue
		}
		valid := false
		switch kind {
		case "number":
			number, ok := value.(float64)
			valid = ok && number >= 0 && number == float64(int64(number))
		case "bool":
			_, valid = value.(bool)
		case "string":
			text, ok := value.(string)
			valid = ok && (text == "true" || text == "false" || text == "admins-only")
		}
		if !valid {
			return nil, fmt.Errorf("%w: unsupported value for %s", ErrInvalidProfile, key)
		}
		settings[key] = value
	}
	return settings, nil
}

// Caller owns the profile locks. New presets are reusable definitions; the
// legacy world-bearing snapshots remain intact for the migration work.
func createServerPreset(serverID, name, description string) (ProfileState, error) {
	serverLifecycleMutex.Lock()
	defer serverLifecycleMutex.Unlock()
	var server *Server
	if manager := GetServerManager(); manager != nil {
		var found bool
		server, found = manager.GetServer(serverID)
		if !found {
			return ProfileState{}, ErrServerMissing
		}
	} else if serverID == DefaultServerID {
		server = GetFactorioServer()
	} else {
		return ProfileState{}, ErrServerMissing
	}
	if server.IsBusy() || WorldGenerationBusy() {
		return ProfileState{}, ErrProfileServerActive
	}
	name, description, err := validateProfileText(name, description)
	if err != nil {
		return ProfileState{}, err
	}
	manifest, err := loadProfileManifest()
	if err != nil {
		return ProfileState{}, err
	}
	if profileNameExists(manifest, name, "") {
		return ProfileState{}, ErrProfileNameConflict
	}
	snapshot := server.ConfigurationSnapshot()
	if snapshot.Version == NilVersion {
		return ProfileState{}, fmt.Errorf("%w: target Factorio version is unknown", ErrInvalidProfile)
	}
	modsDir, configDir := snapshot.ModsDir, snapshot.ConfigDir
	if serverID == DefaultServerID {
		legacy := profileActiveDirectories()
		if modsDir == "" {
			modsDir = legacy["mods"]
		}
		if configDir == "" {
			configDir = legacy["config"]
		}
	}
	if modsDir == "" || configDir == "" {
		return ProfileState{}, fmt.Errorf("%w: target storage is not configured", ErrInvalidProfile)
	}
	info, err := os.Lstat(configDir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return ProfileState{}, fmt.Errorf("%w: target configuration is unavailable", ErrInvalidProfile)
	}
	settings := server.SettingsSnapshot()
	var storedSettings map[string]interface{}
	if err := readBackupJSON(filepath.Join(configDir, "server-settings.json"), &storedSettings); err == nil {
		settings = storedSettings
	} else if !errors.Is(err, os.ErrNotExist) {
		return ProfileState{}, fmt.Errorf("%w: target settings cannot be read", ErrInvalidProfile)
	}
	if settings == nil {
		return ProfileState{}, fmt.Errorf("%w: target settings must be an object", ErrInvalidProfile)
	}
	settings, err = presetSettingValues(settings)
	if err != nil {
		return ProfileState{}, err
	}
	id, err := newProfileID()
	if err != nil {
		return ProfileState{}, err
	}
	now := profileNow().UTC()
	profile := Profile{ID: id, Name: name, Description: description, CreatedAt: now, UpdatedAt: now,
		IsGlobal: true, PresetOnly: true, ReleaseTarget: snapshot.Version.ReleaseString(), InstalledVersion: snapshot.Version.ReleaseString()}
	if err := writeProfileData(id, nil, func(root string) error {
		stagedMods := filepath.Join(root, "mods")
		if err := os.MkdirAll(stagedMods, 0700); err != nil {
			return err
		}
		excluded, err := presetModExclusions(modsDir)
		if err != nil {
			return fmt.Errorf("%w: unable to list target mods", ErrInvalidProfile)
		}
		if err := copyPresetDirectory(modsDir, stagedMods, &backupCopyBudget{}, excluded); err != nil {
			return fmt.Errorf("%w: unable to capture target mods", ErrInvalidProfile)
		}
		preview, err := validateBackupMods(stagedMods, presetAdditionalBuiltIns(snapshot.Version)...)
		if err != nil {
			return fmt.Errorf("%w: target mods cannot form a preset", ErrInvalidProfile)
		}
		profile.GameMode, profile.EnabledBuiltInMods = preview.GameMode, preview.EnabledBuiltInMods
		profile.ModCount = len(preview.Mods)
		if err := os.MkdirAll(filepath.Join(root, "config"), 0700); err != nil {
			return err
		}
		return writeBackupJSON(filepath.Join(root, "config", "server-settings.json"), settings)
	}, "mods", "config"); err != nil {
		return ProfileState{}, err
	}
	manifest.Profiles = append(manifest.Profiles, profile)
	if err := saveProfileManifest(manifest); err != nil {
		_ = os.RemoveAll(profileDirectory(id))
		return ProfileState{}, err
	}
	return stateFromManifest(manifest), nil
}

// Caller owns profileDataGate and profileMutex. Legacy world-bearing snapshots
// remain untouched: only their mods and supported settings are used as a preset.
func assignProfileToServer(serverID, profileID string) (err error) {
	if err := validateProfileID(profileID); err != nil {
		return ErrProfileNotFound
	}
	serverLifecycleMutex.Lock()
	defer serverLifecycleMutex.Unlock()
	if err := ensureServerStopped(serverID); err != nil {
		return err
	}
	manager := GetServerManager()
	server, ok := manager.GetServer(serverID)
	if !ok {
		return ErrServerMissing
	}
	manifest, err := loadProfileManifest()
	if err != nil {
		return err
	}
	index := profileIndex(manifest, profileID)
	if index < 0 {
		return ErrProfileNotFound
	}
	profile := manifest.Profiles[index]
	if server.ModsDir == "" || server.ConfigDir == "" {
		return fmt.Errorf("%w: target storage is not configured", ErrInvalidProfile)
	}
	root := profileDirectory(profileID)
	directories := []string{root, server.ModsDir, server.ConfigDir}
	for i, directory := range directories {
		info, statErr := os.Lstat(directory)
		if statErr != nil && !errors.Is(statErr, os.ErrNotExist) || statErr == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
			return fmt.Errorf("%w: preset and target require real directories", ErrInvalidProfile)
		}
		for j, other := range directories {
			if i == j {
				continue
			}
			relative, relErr := filepath.Rel(directory, other)
			if relErr == nil && (relative == "." || relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))) {
				return fmt.Errorf("%w: preset and target storage must not overlap", ErrInvalidProfile)
			}
		}
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: preset directory is unavailable", ErrInvalidProfile)
	}
	info, err = os.Lstat(filepath.Join(root, "config"))
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: preset configuration is unavailable", ErrInvalidProfile)
	}
	var sourceSettings map[string]interface{}
	if err := readBackupJSON(filepath.Join(root, "config", "server-settings.json"), &sourceSettings); err != nil || sourceSettings == nil {
		return fmt.Errorf("%w: preset settings are invalid", ErrInvalidProfile)
	}
	settings := server.SettingsSnapshot()
	if err := readBackupJSON(filepath.Join(server.ConfigDir, "server-settings.json"), &settings); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%w: target settings cannot be read", ErrInvalidProfile)
	}
	if settings == nil {
		return fmt.Errorf("%w: target settings must be an object", ErrInvalidProfile)
	}
	selected, err := presetSettingValues(sourceSettings)
	if err != nil {
		return err
	}
	for key, value := range selected {
		settings[key] = value
	}

	swaps := make([]*directoryEntrySwap, 0, 2)
	defer func() {
		if err != nil {
			for i := len(swaps) - 1; i >= 0; i-- {
				if swaps[i].isActive {
					err = errors.Join(err, swaps[i].rollback())
				}
			}
		}
		cleanupProfileSwaps(swaps)
	}()
	budget := &backupCopyBudget{}
	mods, err := newDirectoryEntrySwap(server.ModsDir, ".profile-staging-", ".profile-backup-")
	if err != nil {
		return err
	}
	swaps = append(swaps, mods)
	excluded, err := presetModExclusions(filepath.Join(root, "mods"))
	if err != nil {
		return fmt.Errorf("%w: unable to list preset mods", ErrInvalidProfile)
	}
	if err := copyPresetDirectory(filepath.Join(root, "mods"), mods.staging, budget, excluded); err != nil {
		return fmt.Errorf("%w: unable to stage preset mods", ErrInvalidProfile)
	}
	version := server.Snapshot().Version
	if version == NilVersion {
		return fmt.Errorf("%w: target Factorio version is unknown", ErrInvalidProfile)
	}
	preview, err := validateBackupMods(mods.staging, presetAdditionalBuiltIns(version)...)
	if err != nil {
		return fmt.Errorf("%w: preset mods are invalid", ErrInvalidProfile)
	}
	if profile.InstalledVersion != "" {
		var required Version
		if required.UnmarshalText([]byte(profile.InstalledVersion)) != nil || !version.SameFactorioLine(required) {
			return fmt.Errorf("%w: preset requires Factorio %s; target is %s", ErrInvalidProfile, profile.InstalledVersion, version.ReleaseString())
		}
	}
	if len(preview.EnabledBuiltInMods) > 0 {
		return fmt.Errorf("%w: built-in feature presets require per-server feature availability validation", ErrInvalidProfile)
	}
	modSet, err := readMergeModSet(mods.staging)
	if err != nil {
		return fmt.Errorf("%w: preset mod versions or pins are invalid", ErrInvalidProfile)
	}
	versions := map[string]Version{"base": version, "core": version}
	modSet.states["core"] = true
	for name, mod := range modSet.mods {
		var installed Version
		if installed.UnmarshalText([]byte(mod.Version)) != nil || modSet.states[name] && !version.SameFactorioLine(mod.FactorioVersion) {
			return fmt.Errorf("%w: a preset mod is incompatible with Factorio %s", ErrInvalidProfile, version.ReleaseString())
		}
		versions[name] = installed
	}
	for name, mod := range modSet.mods {
		if !modSet.states[name] {
			continue
		}
		for _, raw := range mod.Dependencies {
			dependency, parseErr := ParseModDependency(raw)
			if parseErr != nil {
				return fmt.Errorf("%w: a preset dependency declaration is invalid", ErrInvalidProfile)
			}
			installed, exists := versions[dependency.Name]
			present := exists && modSet.states[dependency.Name]
			matches := present && matchesAllConstraints(installed, []ParsedModDependency{dependency})
			if dependency.Kind == ModDependencyRequired && !matches || dependency.Kind == ModDependencyIncompatible && matches ||
				dependency.Kind != ModDependencyRequired && dependency.Kind != ModDependencyIncompatible && present && !matches {
				return fmt.Errorf("%w: preset mod dependencies conflict", ErrInvalidProfile)
			}
		}
	}
	config, err := newDirectoryEntrySwap(server.ConfigDir, ".profile-staging-", ".profile-backup-")
	if err != nil {
		return err
	}
	swaps = append(swaps, config)
	ignored := map[string]bool{filepath.Base(config.staging): true, filepath.Base(config.backup): true}
	if err := copyPresetDirectory(server.ConfigDir, config.staging, budget, ignored); err != nil {
		return fmt.Errorf("%w: unable to stage target configuration", ErrInvalidProfile)
	}
	if err := writeBackupJSON(filepath.Join(config.staging, "server-settings.json"), settings); err != nil {
		return err
	}
	for _, swap := range swaps {
		if err := swap.activate(); err != nil {
			return fmt.Errorf("activate preset: %w", err)
		}
	}

	// The source manifest is immutable during application. Commit the target's
	// catalog linkage before publishing new runtime settings; retain both old
	// directory backups until the catalog replacement has succeeded.
	manager.mu.Lock()
	found := false
	for i := range manager.catalog.Servers {
		if manager.catalog.Servers[i].ID != server.ID || manager.servers[server.ID] != server {
			continue
		}
		previous := manager.catalog.Servers[i]
		manager.catalog.Servers[i].ProfileID = profileID
		err = manager.saveCatalogLocked()
		if err != nil {
			manager.catalog.Servers[i] = previous
		}
		found = true
		break
	}
	manager.mu.Unlock()
	if !found {
		return ErrServerMissing
	}
	if err != nil {
		return err
	}
	stateMutex.Lock()
	server.Settings = settings
	server.ProfileID = profileID
	stateMutex.Unlock()
	for _, swap := range swaps {
		if cleanupErr := swap.commit(); cleanupErr != nil {
			log.Printf("Preset committed but temporary transaction cleanup failed")
		}
	}
	return nil
}

func copyPresetDirectory(source, destination string, budget *backupCopyBudget, ignored map[string]bool) error {
	info, err := os.Lstat(source)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return ErrInvalidProfile
	}
	return filepath.Walk(source, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil || relative == "." {
			return err
		}
		if ignored[relative] {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if ValidatePathElement(info.Name()) != nil || info.Mode()&os.ModeSymlink != 0 {
			return ErrInvalidProfile
		}
		if info.IsDir() {
			if budget.entries >= backupMaxEntries {
				return ErrInvalidProfile
			}
			budget.entries++
			return os.MkdirAll(filepath.Join(destination, relative), 0700)
		}
		return budget.copy(path, filepath.Join(destination, relative))
	})
}
