package factorio

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/ini.v1"
)

func setupRuntimeManager(t *testing.T) (*profileTestEnvironment, *ServerManager) {
	t.Helper()
	env := setupProfileTest(t)
	defaultServer := GetFactorioServer()
	stateMutex.Lock()
	defaultServer.Version = Version{2, 1, 20}
	defaultServer.BaseModVersion = "2.1.20"
	defaultServer.Settings = map[string]interface{}{"name": "Private default", "game_password": "default-private", "username": "default-account", "admins": []string{"default-admin"}}
	stateMutex.Unlock()
	manager := &ServerManager{
		catalogFile: filepath.Join(env.root, "servers.json"),
		catalog:     ServerCatalog{Version: 1, Servers: []ServerRecord{{ID: DefaultServerID, Name: "Default", Port: 34197, RconPort: 40000, RconPass: "default-rcon"}}},
		servers:     map[string]*Server{DefaultServerID: defaultServer},
	}
	require.NoError(t, manager.saveCatalogLocked())
	return env, manager
}

func TestNewManagedRuntimeHasPrivateDefaultsAndActualVersion(t *testing.T) {
	env, manager := setupRuntimeManager(t)
	before, err := os.ReadFile(filepath.Join(env.active["config"], "server-settings.json"))
	require.NoError(t, err)
	stateMutex.Lock()
	GetFactorioServer().Running = true
	stateMutex.Unlock()
	t.Cleanup(func() { stateMutex.Lock(); GetFactorioServer().Running = false; stateMutex.Unlock() })
	server, err := manager.CreateServer("Independent game", "stable", "127.0.0.1", 34198, false)
	require.NoError(t, err)
	require.Equal(t, "2", server.ID)
	root := filepath.Join(env.root, "servers", server.ID)
	require.Equal(t, filepath.Join(root, "saves"), server.SavesDir)
	require.Equal(t, filepath.Join(root, "mods"), server.ModsDir)
	require.Equal(t, filepath.Join(root, "config"), server.ConfigDir)
	require.Equal(t, Version{2, 1, 20}, server.Version)
	require.Equal(t, "2.1.20", server.BaseModVersion)
	require.True(t, GetFactorioServer().GetRunning(), "creating another runtime stopped the default game")
	settings := server.SettingsSnapshot()
	require.Equal(t, "Independent game", settings["name"])
	require.NotContains(t, settings, "game_password")
	require.NotContains(t, settings, "username")
	require.Empty(t, settings["admins"])
	require.NotEqual(t, "default-rcon", server.RconPass)
	require.FileExists(t, server.SettingsFile())
	require.FileExists(t, filepath.Join(server.ConfigDir, "server-adminlist.json"))
	config, err := ini.Load(filepath.Join(server.ConfigDir, "config.ini"))
	require.NoError(t, err)
	require.Equal(t, filepath.ToSlash(root), config.Section("path").Key("write-data").String())
	iniBytes, err := os.ReadFile(filepath.Join(server.ConfigDir, "config.ini"))
	require.NoError(t, err)
	require.Contains(t, string(iniBytes), "read-data="+config.Section("path").Key("read-data").String())
	require.Contains(t, string(iniBytes), "write-data="+filepath.ToSlash(root))
	var mods ModSimpleList
	require.NoError(t, readBackupJSON(filepath.Join(server.ModsDir, "mod-list.json"), &mods))
	for _, mod := range mods.Mods {
		require.Equal(t, mod.Name == "base", mod.Enabled)
	}
	entries, err := os.ReadDir(server.SavesDir)
	require.NoError(t, err)
	require.Empty(t, entries, "new servers must not clone the default world's saves")
	after, err := os.ReadFile(filepath.Join(env.active["config"], "server-settings.json"))
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestManagedRuntimeRestoresOnlyItsOwnSettingsAndAdmins(t *testing.T) {
	env, manager := setupRuntimeManager(t)
	server, err := manager.CreateServer("Worker", "stable", "127.0.0.1", 34198, false)
	require.NoError(t, err)
	root := filepath.Join(env.root, "servers", server.ID)
	require.NoError(t, os.MkdirAll(filepath.Join(root, "config"), 0700))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "saves"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "config", "server-settings.json"), []byte(`{"name":"Worker world","game_password":"worker-private","max_players":7}`), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "config", "server-adminlist.json"), []byte(`["worker-admin"]`), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "saves", "worker.zip"), []byte("worker-owned world"), 0600))
	manager.catalog.Servers[1].Savefile = "worker.zip"
	manager.catalog.Servers[1].UseMimalloc = true
	require.NoError(t, manager.saveCatalogLocked())
	restored := &ServerManager{catalogFile: manager.catalogFile}
	require.NoError(t, restored.loadCatalog())
	require.NoError(t, restored.rebuildServers())
	worker, ok := restored.GetServer(server.ID)
	require.True(t, ok)
	require.Equal(t, "worker-private", worker.SettingsSnapshot()["game_password"])
	require.Equal(t, []interface{}{"worker-admin"}, worker.SettingsSnapshot()["admins"])
	require.Equal(t, float64(7), worker.SettingsSnapshot()["max_players"])
	require.Equal(t, "worker.zip", worker.Savefile)
	require.True(t, worker.UseMimalloc)
	require.Equal(t, Version{2, 1, 20}, worker.Version)
	assertFileContents(t, filepath.Join(root, "saves", "worker.zip"), "worker-owned world")
}

func TestManagedRuntimeRejectsInvalidSettingsBeforePublication(t *testing.T) {
	for _, invalid := range []string{`null`, `[]`, `{"incomplete":`} {
		t.Run(invalid, func(t *testing.T) {
			env, manager := setupRuntimeManager(t)
			server, err := manager.CreateServer("Worker", "stable", "127.0.0.1", 34198, false)
			require.NoError(t, err)
			path := filepath.Join(env.root, "servers", server.ID, "config", "server-settings.json")
			require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
			require.NoError(t, os.WriteFile(path, []byte(invalid), 0600))
			beforeDefault := GetFactorioServer().ConfigurationSnapshot()
			require.Error(t, manager.rebuildServers())
			afterDefault := GetFactorioServer().ConfigurationSnapshot()
			require.Equal(t, beforeDefault.ID, afterDefault.ID)
			require.Equal(t, beforeDefault.Name, afterDefault.Name)
			stillPublished, ok := manager.GetServer(server.ID)
			require.True(t, ok)
			require.Same(t, server, stillPublished)
			assertFileContents(t, path, invalid)
		})
	}
}

func TestManagedRuntimeCreationFailureLeavesCatalogAndStorageUntouched(t *testing.T) {
	env, manager := setupRuntimeManager(t)
	before, err := os.ReadFile(manager.catalogFile)
	require.NoError(t, err)
	original := atomicFileRename
	t.Cleanup(func() { atomicFileRename = original })
	atomicFileRename = func(source, destination string) error {
		if destination == manager.catalogFile && strings.HasPrefix(filepath.Base(source), ".fsm-write-") {
			return errors.New("synthetic new runtime catalog failure")
		}
		return os.Rename(source, destination)
	}
	_, err = manager.CreateServer("Must not publish", "stable", "127.0.0.1", 34198, false)
	require.Error(t, err)
	if len(manager.catalog.Servers) != 1 || len(manager.servers) != 1 {
		t.Fatal("failed creation published a runtime or catalog entry")
	}
	after, err := os.ReadFile(manager.catalogFile)
	require.NoError(t, err)
	require.Equal(t, before, after)
	require.NoDirExists(t, filepath.Join(env.root, "servers", "2"))
}

func TestNewManagedRuntimeDoesNotClaimOrphanedStorage(t *testing.T) {
	env, manager := setupRuntimeManager(t)
	orphan := filepath.Join(env.root, "servers", "2", "saves")
	require.NoError(t, os.MkdirAll(orphan, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(orphan, "private.zip"), []byte("retained world"), 0600))
	server, err := manager.CreateServer("Fresh identity", "stable", "127.0.0.1", 34198, false)
	require.NoError(t, err)
	require.Equal(t, "3", server.ID)
	assertFileContents(t, filepath.Join(orphan, "private.zip"), "retained world")
}

func TestCatalogRejectsUnsafeAndDuplicateServerIDs(t *testing.T) {
	for _, ids := range [][]string{{"../escape"}, {"CON"}, {""}, {"2", "2"}} {
		t.Run(strings.Join(ids, ","), func(t *testing.T) {
			_, manager := setupRuntimeManager(t)
			manager.catalog.Servers = nil
			for _, id := range ids {
				manager.catalog.Servers = append(manager.catalog.Servers, ServerRecord{ID: id})
			}
			require.NoError(t, manager.saveCatalogLocked())
			loaded := &ServerManager{catalogFile: manager.catalogFile}
			require.Error(t, loaded.loadCatalog())
		})
	}
}

func TestManagedRuntimeConfigRepairPreservesLocalOptionsAndRollsBack(t *testing.T) {
	_, manager := setupRuntimeManager(t)
	server, err := manager.CreateServer("Worker", "stable", "127.0.0.1", 34198, false)
	require.NoError(t, err)
	path := filepath.Join(server.ConfigDir, "config.ini")
	previous := "[path]\nread-data=/old/data\nwrite-data=/wrong/shared/root\n[other]\nresearch-queue-size=5\n"
	require.NoError(t, os.WriteFile(path, []byte(previous), 0600))
	rootInfo, err := os.Stat(server.ConfigDir)
	require.NoError(t, err)
	original := profileRename
	t.Cleanup(func() { profileRename = original })
	profileRename = func(source, destination string) error {
		if destination == path && strings.Contains(source, ".runtime-staging-") {
			return errors.New("synthetic runtime config activation failure")
		}
		return os.Rename(source, destination)
	}
	require.Error(t, manager.rebuildServers())
	assertFileContents(t, path, previous)
	profileRename = original
	require.NoError(t, manager.rebuildServers())
	config, err := ini.Load(path)
	require.NoError(t, err)
	require.Equal(t, filepath.ToSlash(filepath.Dir(server.ConfigDir)), config.Section("path").Key("write-data").String())
	require.Equal(t, "5", config.Section("other").Key("research-queue-size").String())
	after, err := os.Stat(server.ConfigDir)
	require.NoError(t, err)
	require.True(t, os.SameFile(rootInfo, after), "runtime config mount root was replaced")
}

func TestManagedRuntimeRejectsUninstalledExactVersion(t *testing.T) {
	env, manager := setupRuntimeManager(t)
	_, err := manager.CreateServer("Wrong engine", "2.0.77", "127.0.0.1", 34198, false)
	require.ErrorIs(t, err, ErrInvalidServerConfiguration)
	if len(manager.catalog.Servers) != 1 {
		t.Fatal("unsupported engine target was published")
	}
	require.NoDirExists(t, filepath.Join(env.root, "servers", "2"))
}

func TestManagedRuntimeRejectsSymlinkedStorageParent(t *testing.T) {
	env, manager := setupRuntimeManager(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(env.root, "servers")); err != nil {
		t.Skip("creating a directory symlink requires platform privileges")
	}
	_, err := manager.CreateServer("Must not escape", "stable", "127.0.0.1", 34198, false)
	require.Error(t, err)
	entries, err := os.ReadDir(outside)
	require.NoError(t, err)
	require.Empty(t, entries)
}

func TestManagedRuntimeRebuildRefreshesSharedVersionWithoutCloningWorld(t *testing.T) {
	_, manager := setupRuntimeManager(t)
	server, err := manager.CreateServer("Worker", "stable", "127.0.0.1", 34198, false)
	require.NoError(t, err)
	stateMutex.Lock()
	GetFactorioServer().Version = Version{2, 0, 77}
	GetFactorioServer().BaseModVersion = "2.0.77"
	stateMutex.Unlock()
	require.NoError(t, manager.rebuildServers())
	restored, ok := manager.GetServer(server.ID)
	require.True(t, ok)
	require.Equal(t, Version{2, 0, 77}, restored.Version)
	entries, err := os.ReadDir(restored.SavesDir)
	require.NoError(t, err)
	require.Empty(t, entries, "hydration must not clone Server 1's saved world")
}

func TestManagedRuntimeFailedSelectionPersistenceDoesNotStartOrPublish(t *testing.T) {
	_, manager := setupRuntimeManager(t)
	server, err := manager.CreateServer("Worker", "stable", "127.0.0.1", 34198, false)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(server.SavesDir, "worker.zip"), []byte("owned test world"), 0600))
	before, err := os.ReadFile(manager.catalogFile)
	require.NoError(t, err)
	original := atomicFileRename
	t.Cleanup(func() { atomicFileRename = original })
	atomicFileRename = func(source, destination string) error {
		if destination == manager.catalogFile && strings.HasPrefix(filepath.Base(source), ".fsm-write-") {
			return errors.New("synthetic selection persistence failure")
		}
		return os.Rename(source, destination)
	}
	_, err = server.Start("127.0.0.1", 34198, "worker.zip")
	require.Error(t, err)
	require.False(t, server.IsBusy())
	require.Empty(t, server.Savefile)
	require.Empty(t, manager.catalog.Servers[1].Savefile)
	after, err := os.ReadFile(manager.catalogFile)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestManagedRuntimeDeletionRetainsIdentityOnPersistenceFailure(t *testing.T) {
	_, manager := setupRuntimeManager(t)
	server, err := manager.CreateServer("Worker", "stable", "127.0.0.1", 34198, false)
	require.NoError(t, err)
	server.Running = true
	require.ErrorIs(t, manager.DeleteServer(server.ID), ErrServerActive)
	server.Running = false
	original := atomicFileRename
	t.Cleanup(func() { atomicFileRename = original })
	atomicFileRename = func(source, destination string) error {
		if destination == manager.catalogFile && strings.HasPrefix(filepath.Base(source), ".fsm-write-") {
			return errors.New("synthetic deletion persistence failure")
		}
		return os.Rename(source, destination)
	}
	require.Error(t, manager.DeleteServer(server.ID))
	retained, ok := manager.GetServer(server.ID)
	require.True(t, ok)
	require.Same(t, server, retained)
	if len(manager.catalog.Servers) != 2 {
		t.Fatal("failed deletion removed its catalog identity")
	}
}

func TestStoppedManagedRuntimeKillDoesNotPanicOrChangeState(t *testing.T) {
	server := &Server{ID: "2", Settings: map[string]interface{}{}}
	require.NotPanics(t, func() { require.NoError(t, server.Kill()) })
	require.False(t, server.IsBusy())
}

func TestWindowsRejectsConcurrentRuntimesUntilConsoleIsolation(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows console process-control boundary")
	}
	_, manager := setupRuntimeManager(t)
	server, err := manager.CreateServer("Worker", "stable", "127.0.0.1", 34198, false)
	require.NoError(t, err)
	stateMutex.Lock()
	GetFactorioServer().Running = true
	stateMutex.Unlock()
	t.Cleanup(func() { stateMutex.Lock(); GetFactorioServer().Running = false; stateMutex.Unlock() })
	_, err = server.Start("127.0.0.1", 34198, "worker.zip")
	require.ErrorIs(t, err, ErrInvalidServerConfiguration)
	require.True(t, GetFactorioServer().GetRunning())
	require.False(t, server.IsBusy())
}

func TestSharedEngineChangesRejectAnyRunningManagedGame(t *testing.T) {
	_, manager := setupRuntimeManager(t)
	server, err := manager.CreateServer("Worker", "stable", "127.0.0.1", 34198, false)
	require.NoError(t, err)
	serverManagerMu.Lock()
	previous := serverManager
	serverManager = manager
	serverManagerMu.Unlock()
	t.Cleanup(func() {
		stateMutex.Lock()
		server.Running = false
		stateMutex.Unlock()
		serverManagerMu.Lock()
		serverManager = previous
		serverManagerMu.Unlock()
	})
	stateMutex.Lock()
	server.Running = true
	stateMutex.Unlock()
	require.False(t, GetFactorioServer().GetRunning())
	require.ErrorIs(t, ensureProfileServerStopped(), ErrProfileServerActive)
	require.ErrorIs(t, InstallRelease("stable"), ErrProfileServerActive, "a shared-engine change must fail before any download or mutation")
}

func TestManagedRuntimeINIIsNativeCompatibleAndIdempotent(t *testing.T) {
	previous := []byte("[path]\r\nread-data  = /old/data\r\nwrite-data = /shared/default\r\n[other]\r\nresearch-queue-size=5\r\n")
	updated, err := managedRuntimeINI(previous, "/installed/data", "/owned/server")
	require.NoError(t, err)
	require.Contains(t, string(updated), "read-data=/installed/data\nwrite-data=/owned/server")
	require.Contains(t, string(updated), "research-queue-size=5")
	again, err := managedRuntimeINI(updated, "/installed/data", "/owned/server")
	require.NoError(t, err)
	require.Equal(t, updated, again)
	_, err = managedRuntimeINI(nil, "/installed/data", "/bad\npath")
	require.ErrorIs(t, err, ErrInvalidServerConfiguration)
}

func TestManagedSettingsWriteUsesOwnedFilesOnly(t *testing.T) {
	env, manager := setupRuntimeManager(t)
	worker, err := manager.CreateServer("Worker", "stable", "127.0.0.1", 34198, false)
	require.NoError(t, err)
	require.ErrorIs(t, worker.UpdateServerSettings(nil), ErrInvalidServerConfiguration)
	defaultPath := filepath.Join(env.active["config"], "server-settings.json")
	before, err := os.ReadFile(defaultPath)
	require.NoError(t, err)
	settings := worker.SettingsSnapshot()
	settings["name"] = "Worker private game"
	settings["admins"] = []string{"worker-admin"}
	require.NoError(t, worker.UpdateServerSettings(settings))
	var owned map[string]interface{}
	require.NoError(t, readBackupJSON(worker.SettingsFile(), &owned))
	require.Equal(t, "Worker private game", owned["name"])
	var admins []string
	require.NoError(t, readBackupJSON(filepath.Join(worker.ConfigDir, "server-adminlist.json"), &admins))
	require.Equal(t, []string{"worker-admin"}, admins)
	after, err := os.ReadFile(defaultPath)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestManagedRuntimeConfigAtMountedDestination(t *testing.T) {
	root := os.Getenv("FSM_TEST_MOUNTED_MANAGER_DIR")
	if root == "" {
		t.Skip("requires disposable mounted manager and worker config directories")
	}
	_, manager := setupRuntimeManager(t)
	manager.catalogFile = filepath.Join(root, "servers.json")
	manager.catalog.Servers = append(manager.catalog.Servers, ServerRecord{ID: "2", Name: "Mounted worker", Port: 34198, RconPort: 40001, RconPass: "synthetic-mounted-rcon"})
	require.NoError(t, manager.saveCatalogLocked())
	configDir := filepath.Join(root, "servers", "2", "config")
	mountBefore, err := os.Stat(configDir)
	require.NoError(t, err)
	require.NoError(t, manager.rebuildServers())
	path := filepath.Join(configDir, "config.ini")
	previous := "[path]\nread-data=/old/data\nwrite-data=/old/shared\n[other]\nlocal-option=preserve\n"
	require.NoError(t, os.WriteFile(path, []byte(previous), 0600))
	original := profileRename
	t.Cleanup(func() { profileRename = original })
	profileRename = func(source, destination string) error {
		if destination == path && strings.Contains(source, ".runtime-staging-") {
			return errors.New("synthetic mounted runtime activation failure")
		}
		return os.Rename(source, destination)
	}
	require.Error(t, manager.rebuildServers())
	assertFileContents(t, path, previous)
	profileRename = original
	require.NoError(t, manager.rebuildServers())
	contents, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(contents), "write-data="+filepath.ToSlash(filepath.Dir(configDir)))
	require.Contains(t, string(contents), "local-option=preserve")
	mountAfter, err := os.Stat(configDir)
	require.NoError(t, err)
	require.True(t, os.SameFile(mountBefore, mountAfter))
}

func TestManagedRuntimeStartCannotOverrideItsDedicatedPort(t *testing.T) {
	_, manager := setupRuntimeManager(t)
	server, err := manager.CreateServer("Worker", "stable", "127.0.0.1", 34198, false)
	require.NoError(t, err)
	_, err = server.Start("127.0.0.1", 34197, "worker.zip")
	require.ErrorIs(t, err, ErrInvalidServerConfiguration)
	require.Equal(t, 34198, server.Port)
	require.False(t, server.IsBusy())
}
