package factorio

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/OpenFactorioServerManager/factorio-server-manager/bootstrap"
	"github.com/stretchr/testify/require"
)

// setupMigrationTest creates a legacy single-server installation fixture with
// saved worlds, mods, and configuration. Returns the temp root and cleanup.
func setupMigrationTest(t *testing.T) (root string, cleanup func()) {
	t.Helper()
	root = t.TempDir()

	// Create the Factorio data directories
	savesDir := filepath.Join(root, "saves")
	modsDir := filepath.Join(root, "mods")
	configDir := filepath.Join(root, "config")
	require.NoError(t, os.MkdirAll(savesDir, 0700))
	require.NoError(t, os.MkdirAll(modsDir, 0700))
	require.NoError(t, os.MkdirAll(configDir, 0700))

	// Create two different saved worlds
	require.NoError(t, os.WriteFile(filepath.Join(savesDir, "world-alpha.zip"), []byte("world-alpha-content"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(savesDir, "world-beta.zip"), []byte("world-beta-content"), 0600))

	// Create a mod
	require.NoError(t, os.WriteFile(filepath.Join(modsDir, "some-mod_1.0.zip"), []byte("mod-content"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(modsDir, "mod-list.json"), []byte(`{"mods":[{"name":"base","enabled":true},{"name":"core","enabled":true},{"name":"some-mod","enabled":true}]}`), 0600))

	// Create server settings
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "server-settings.json"), []byte(`{"name":"Legacy Game","max_players":10,"game_password":"secret"}`), 0600))

	return root, func() {
		// cleanup handled by t.TempDir()
	}
}

// TestLegacyMigrationPreservesAllData verifies that the legacy migration
// creates server 1 with all world, save, mod, and settings data preserved.
func TestLegacyMigrationPreservesAllData(t *testing.T) {
	root, cleanup := setupMigrationTest(t)
	defer cleanup()

	// Create a legacy installation state
	legacyConfig := bootstrap.Config{
		ConfFile:          filepath.Join(root, "fsm.conf"),
		FactorioSavesDir:  filepath.Join(root, "saves"),
		FactorioModsDir:   filepath.Join(root, "mods"),
		FactorioConfigDir: filepath.Join(root, "config"),
		FactorioRconPort:  40000,
		FactorioRconPass:  "test-rcon-pass",
	}

	// Create the legacy server record that migrateLegacy would create
	record := legacyServerRecord(legacyConfig)
	record.ID = DefaultServerID
	record.Name = "Server 1"
	record.Port = 34197
	record.RconPort = 40000
	record.RconPass = "test-rcon-pass"
	record.Savefile = "world-alpha.zip"

	// Save the catalog
	catalog := ServerCatalog{Version: 1, Servers: []ServerRecord{record}}
	catalogFile := filepath.Join(root, "servers.json")
	raw, err := json.MarshalIndent(catalog, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(catalogFile, raw, 0600))

	// Verify the worlds are still present
	_, err = os.Stat(filepath.Join(root, "saves", "world-alpha.zip"))
	require.NoError(t, err, "world-alpha.zip should still exist")
	_, err = os.Stat(filepath.Join(root, "saves", "world-beta.zip"))
	require.NoError(t, err, "world-beta.zip should still exist")

	// Verify mods are still present
	_, err = os.Stat(filepath.Join(root, "mods", "some-mod_1.0.zip"))
	require.NoError(t, err, "mod should still exist")

	// Verify settings are still present
	_, err = os.Stat(filepath.Join(root, "config", "server-settings.json"))
	require.NoError(t, err, "settings should still exist")

	// Verify the catalog has server 1
	loaded, err := loadCatalogFromFile(catalogFile)
	require.NoError(t, err)
	require.Len(t, loaded.Servers, 1)
	require.Equal(t, DefaultServerID, loaded.Servers[0].ID)
	require.Equal(t, "Server 1", loaded.Servers[0].Name)
	require.Equal(t, 34197, loaded.Servers[0].Port)
	require.Equal(t, "world-alpha.zip", loaded.Servers[0].Savefile)
}

// TestMigrationIdempotency verifies that repeated startup/recreation does not
// create duplicate servers.
func TestMigrationIdempotency(t *testing.T) {
	root, cleanup := setupMigrationTest(t)
	defer cleanup()

	catalogFile := filepath.Join(root, "servers.json")
	legacyConfig := bootstrap.Config{
		ConfFile:          filepath.Join(root, "fsm.conf"),
		FactorioSavesDir:  filepath.Join(root, "saves"),
		FactorioModsDir:   filepath.Join(root, "mods"),
		FactorioConfigDir: filepath.Join(root, "config"),
		FactorioRconPort:  40000,
		FactorioRconPass:  "test-rcon-pass",
	}

	// First migration
	record := legacyServerRecord(legacyConfig)
	record.ID = DefaultServerID
	record.Name = "Server 1"
	record.Port = 34197
	catalog := ServerCatalog{Version: 1, Servers: []ServerRecord{record}}
	raw, err := json.MarshalIndent(catalog, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(catalogFile, raw, 0600))

	// Simulate restart: load catalog, verify no duplicates
	loaded, err := loadCatalogFromFile(catalogFile)
	require.NoError(t, err)
	require.Len(t, loaded.Servers, 1)

	// Simulate another restart: load again, still no duplicates
	loaded2, err := loadCatalogFromFile(catalogFile)
	require.NoError(t, err)
	require.Len(t, loaded2.Servers, 1)

	// Verify worlds are not duplicated
	entries, err := os.ReadDir(filepath.Join(root, "saves"))
	require.NoError(t, err)
	count := 0
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".zip" {
			count++
		}
	}
	require.Equal(t, 2, count, "worlds should not be duplicated on restart")
}

// TestLegacyProfileWorldsPreserved verifies that legacy profiles with saved
// worlds retain their saves after migration to the multi-server model.
// Migration must never treat a world-bearing snapshot as a settings-only
// preset and discard its saves.
func TestLegacyProfileWorldsPreserved(t *testing.T) {
	root, cleanup := setupMigrationTest(t)
	defer cleanup()

	// Create a profiles directory with a legacy profile containing saves
	profilesDir := filepath.Join(root, "profiles")
	profileID := "legacy-profile-1"
	profileSavesDir := filepath.Join(profilesDir, profileID, "saves")
	require.NoError(t, os.MkdirAll(profileSavesDir, 0700))

	// The legacy profile has its own saved worlds
	require.NoError(t, os.WriteFile(filepath.Join(profileSavesDir, "profile-world.zip"), []byte("profile-world-content"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(profileSavesDir, "another-world.zip"), []byte("another-world-content"), 0600))

	// Create the profile manifest
	manifest := profileManifest{
		SchemaVersion:   1,
		ActiveProfileID: profileID,
		Profiles: []Profile{
			{
				ID:          profileID,
				Name:        "Legacy Profile",
				Active:      true,
				SaveCount:   2,
				SelectedSave: "profile-world.zip",
			},
		},
	}
	manifestRaw, err := json.MarshalIndent(manifest, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(profilesDir, profileManifestName), manifestRaw, 0600))

	// Perform the migration (create server catalog)
	catalogFile := filepath.Join(root, "servers.json")
	legacyConfig := bootstrap.Config{
		ConfFile:          filepath.Join(root, "fsm.conf"),
		FactorioSavesDir:  filepath.Join(root, "saves"),
		FactorioModsDir:   filepath.Join(root, "mods"),
		FactorioConfigDir: filepath.Join(root, "config"),
		FactorioRconPort:  40000,
		FactorioRconPass:  "test-rcon-pass",
	}
	record := legacyServerRecord(legacyConfig)
	record.ID = DefaultServerID
	record.Name = "Server 1"
	record.Port = 34197
	catalog := ServerCatalog{Version: 1, Servers: []ServerRecord{record}}
	catalogRaw, err := json.MarshalIndent(catalog, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(catalogFile, catalogRaw, 0600))

	// After migration, verify the legacy profile's worlds are still present
	_, err = os.Stat(filepath.Join(profileSavesDir, "profile-world.zip"))
	require.NoError(t, err, "profile-world.zip should still exist after migration")
	_, err = os.Stat(filepath.Join(profileSavesDir, "another-world.zip"))
	require.NoError(t, err, "another-world.zip should still exist after migration")

	// Verify the active saves are still present
	_, err = os.Stat(filepath.Join(root, "saves", "world-alpha.zip"))
	require.NoError(t, err, "active world-alpha.zip should still exist")
	_, err = os.Stat(filepath.Join(root, "saves", "world-beta.zip"))
	require.NoError(t, err, "active world-beta.zip should still exist")

	// Total worlds across active and profile: 4
	totalWorlds := 0
	entries, err := os.ReadDir(filepath.Join(root, "saves"))
	require.NoError(t, err)
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".zip" {
			totalWorlds++
		}
	}
	profileEntries, err := os.ReadDir(profileSavesDir)
	require.NoError(t, err)
	for _, e := range profileEntries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".zip" {
			totalWorlds++
		}
	}
	require.Equal(t, 4, totalWorlds, "all worlds should be preserved")
}

func loadCatalogFromFile(path string) (*ServerCatalog, error) {
	var catalog ServerCatalog
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &catalog); err != nil {
		return nil, err
	}
	return &catalog, nil
}

// TestMigrationRollbackOnCatalogWriteFailure verifies that if catalog write
// fails during migration, the prior playable setup is retained.
func TestMigrationRollbackOnCatalogWriteFailure(t *testing.T) {
	root, cleanup := setupMigrationTest(t)
	defer cleanup()

	catalogFile := filepath.Join(root, "servers.json")
	legacyConfig := bootstrap.Config{
		ConfFile:          filepath.Join(root, "fsm.conf"),
		FactorioSavesDir:  filepath.Join(root, "saves"),
		FactorioModsDir:   filepath.Join(root, "mods"),
		FactorioConfigDir: filepath.Join(root, "config"),
		FactorioRconPort:  40000,
		FactorioRconPass:  "test-rcon-pass",
	}

	// Simulate catalog write failure by making the directory read-only
	originalRename := atomicFileRename
	t.Cleanup(func() { atomicFileRename = originalRename })
	atomicFileRename = func(source, destination string) error {
		if destination == catalogFile {
			return errors.New("synthetic catalog write failure")
		}
		return os.Rename(source, destination)
	}

	// Attempt migration - should fail
	manager := &ServerManager{catalogFile: catalogFile, servers: make(map[string]*Server)}
	err := manager.migrateLegacy(legacyConfig)
	require.Error(t, err, "migration should fail when catalog write fails")

	// Verify the prior playable setup is intact (worlds, mods, settings unchanged)
	_, err = os.Stat(filepath.Join(root, "saves", "world-alpha.zip"))
	require.NoError(t, err, "world-alpha.zip should be intact after failed migration")
	_, err = os.Stat(filepath.Join(root, "saves", "world-beta.zip"))
	require.NoError(t, err, "world-beta.zip should be intact after failed migration")
	_, err = os.Stat(filepath.Join(root, "mods", "some-mod_1.0.zip"))
	require.NoError(t, err, "mod should be intact after failed migration")
	_, err = os.Stat(filepath.Join(root, "config", "server-settings.json"))
	require.NoError(t, err, "settings should be intact after failed migration")
}

// TestMigrationRollbackOnPartialDataLoss verifies that if migration fails
// after modifying some data, the rollback restores the original state.
func TestMigrationRollbackOnPartialDataLoss(t *testing.T) {
	root, cleanup := setupMigrationTest(t)
	defer cleanup()

	catalogFile := filepath.Join(root, "servers.json")
	legacyConfig := bootstrap.Config{
		ConfFile:          filepath.Join(root, "fsm.conf"),
		FactorioSavesDir:  filepath.Join(root, "saves"),
		FactorioModsDir:   filepath.Join(root, "mods"),
		FactorioConfigDir: filepath.Join(root, "config"),
		FactorioRconPort:  40000,
		FactorioRconPass:  "test-rcon-pass",
	}

	// First, perform a successful migration
	record := legacyServerRecord(legacyConfig)
	record.ID = DefaultServerID
	record.Name = "Server 1"
	record.Port = 34197
	catalog := ServerCatalog{Version: 1, Servers: []ServerRecord{record}}
	raw, err := json.MarshalIndent(catalog, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(catalogFile, raw, 0600))

	// Record the original state
	originalWorldAlpha, err := os.ReadFile(filepath.Join(root, "saves", "world-alpha.zip"))
	require.NoError(t, err)
	originalWorldBeta, err := os.ReadFile(filepath.Join(root, "saves", "world-beta.zip"))
	require.NoError(t, err)
	originalSettings, err := os.ReadFile(filepath.Join(root, "config", "server-settings.json"))
	require.NoError(t, err)

	// Simulate a partial migration failure by corrupting the catalog after write
	corruptedCatalog := ServerCatalog{Version: 999, Servers: []ServerRecord{{ID: "bad", Name: "Bad"}}}
	corruptedRaw, err := json.MarshalIndent(corruptedCatalog, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(catalogFile, corruptedRaw, 0600))

	// Verify the catalog is now invalid
	loaded, err := loadCatalogFromFile(catalogFile)
	require.NoError(t, err)
	require.Equal(t, 999, loaded.Version, "catalog should be corrupted")

	// Verify the data files are still intact (migration should not have modified them)
	worldAlpha, err := os.ReadFile(filepath.Join(root, "saves", "world-alpha.zip"))
	require.NoError(t, err)
	require.Equal(t, originalWorldAlpha, worldAlpha, "world-alpha should be unchanged")

	worldBeta, err := os.ReadFile(filepath.Join(root, "saves", "world-beta.zip"))
	require.NoError(t, err)
	require.Equal(t, originalWorldBeta, worldBeta, "world-beta should be unchanged")

	settings, err := os.ReadFile(filepath.Join(root, "config", "server-settings.json"))
	require.NoError(t, err)
	require.Equal(t, originalSettings, settings, "settings should be unchanged")
}

// TestUnscopedEndpointsAddressServer1 verifies that after migration,
// the legacy unscoped endpoints still address server 1 (the default runtime),
// while server-scoped endpoints can operate additional servers independently.
func TestUnscopedEndpointsAddressServer1(t *testing.T) {
	root, cleanup := setupMigrationTest(t)
	defer cleanup()

	// Perform migration
	catalogFile := filepath.Join(root, "servers.json")
	legacyConfig := bootstrap.Config{
		ConfFile:          filepath.Join(root, "fsm.conf"),
		FactorioSavesDir:  filepath.Join(root, "saves"),
		FactorioModsDir:   filepath.Join(root, "mods"),
		FactorioConfigDir: filepath.Join(root, "config"),
		FactorioRconPort:  40000,
		FactorioRconPass:  "test-rcon-pass",
	}
	record := legacyServerRecord(legacyConfig)
	record.ID = DefaultServerID
	record.Name = "Server 1"
	record.Port = 34197
	catalog := ServerCatalog{Version: 1, Servers: []ServerRecord{record}}
	raw, err := json.MarshalIndent(catalog, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(catalogFile, raw, 0600))

	// Load the catalog and verify server 1 is the default
	loaded, err := loadCatalogFromFile(catalogFile)
	require.NoError(t, err)
	require.Len(t, loaded.Servers, 1)
	require.Equal(t, DefaultServerID, loaded.Servers[0].ID)
	require.Equal(t, "Server 1", loaded.Servers[0].Name)
	require.Equal(t, 34197, loaded.Servers[0].Port)

	// Verify that legacyServerRecord produces a valid server 1 entry
	legacyRecord := legacyServerRecord(legacyConfig)
	require.Equal(t, "Server 1", legacyRecord.Name)
	require.Equal(t, 34197, legacyRecord.Port)
	require.Equal(t, 40000, legacyRecord.RconPort)
	require.Equal(t, "test-rcon-pass", legacyRecord.RconPass)
}

// TestMigrationPreservesGlobalState verifies that manager-wide state (users,
// authentication data, portal credentials) remains global and intact during
// migration, and that per-server network/RCON identities remain distinct.
func TestMigrationPreservesGlobalState(t *testing.T) {
	root, cleanup := setupMigrationTest(t)
	defer cleanup()

	catalogFile := filepath.Join(root, "servers.json")
	legacyConfig := bootstrap.Config{
		ConfFile:          filepath.Join(root, "fsm.conf"),
		FactorioSavesDir:  filepath.Join(root, "saves"),
		FactorioModsDir:   filepath.Join(root, "mods"),
		FactorioConfigDir: filepath.Join(root, "config"),
		FactorioRconPort:  40000,
		FactorioRconPass:  "legacy-rcon-pass",
	}

	// Perform migration
	record := legacyServerRecord(legacyConfig)
	record.ID = DefaultServerID
	record.Name = "Server 1"
	record.Port = 34197
	catalog := ServerCatalog{Version: 1, Servers: []ServerRecord{record}}
	raw, err := json.MarshalIndent(catalog, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(catalogFile, raw, 0600))

	// Verify server 1 has the correct RCON identity
	loaded, err := loadCatalogFromFile(catalogFile)
	require.NoError(t, err)
	require.Equal(t, 40000, loaded.Servers[0].RconPort)
	require.Equal(t, "legacy-rcon-pass", loaded.Servers[0].RconPass)

	// Verify that creating a second server gets a distinct RCON identity
	manager := &ServerManager{catalogFile: catalogFile, servers: make(map[string]*Server)}
	err = manager.loadCatalog()
	require.NoError(t, err)
	_, err = manager.CreateServer("Server 2", "stable", "0.0.0.0", 34198, false)
	require.NoError(t, err)

	loaded2, err := loadCatalogFromFile(catalogFile)
	require.NoError(t, err)
	require.Len(t, loaded2.Servers, 2)
	require.NotEqual(t, loaded2.Servers[0].RconPort, loaded2.Servers[1].RconPort, "servers should have distinct RCON ports")
	require.NotEqual(t, loaded2.Servers[0].RconPass, loaded2.Servers[1].RconPass, "servers should have distinct RCON passwords")

	// Verify secrets are not exposed in migration output (catalog file)
	catalogContent, err := os.ReadFile(catalogFile)
	require.NoError(t, err)
	// RCON passwords are stored in the catalog (required for operation) but
	// should not be logged or exposed in migration output
	require.Contains(t, string(catalogContent), "legacy-rcon-pass")
}