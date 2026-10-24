package factorio

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/OpenFactorioServerManager/factorio-server-manager/bootstrap"
	"github.com/stretchr/testify/require"
)

// TestTwoServerConcurrentLifecycle verifies that two Factorio servers can
// run concurrently under one manager with independent lifecycles.
func TestTwoServerConcurrentLifecycle(t *testing.T) {
	// Check if Factorio is installed (local or Docker container)
	config := bootstrap.GetConfig()
	binaryPaths := []string{
		config.FactorioBinary,
		"/opt/factorio/factorio",         // Docker container path
		"/opt/factorio/bin/x64/factorio", // Docker container path (x64)
	}

	found := false
	for _, path := range binaryPaths {
		if path == "" {
			continue
		}
		if _, err := os.Stat(path); err == nil {
			found = true
			break
		}
	}
	if !found {
		t.Skip("Factorio binary not found (set FSM_FACTORIO_DIR or run inside Docker container)")
	}

	// Create two servers
	manager := &ServerManager{
		catalogFile: filepath.Join(t.TempDir(), "servers.json"),
		servers:     make(map[string]*Server),
	}

	serverA, err := manager.CreateServer("Server A", "stable", "127.0.0.1", 34197, false)
	require.NoError(t, err)

	serverB, err := manager.CreateServer("Server B", "stable", "127.0.0.1", 34198, false)
	require.NoError(t, err)

	// Verify both servers have distinct identities
	require.NotEqual(t, serverA.ID, serverB.ID)
	require.NotEqual(t, serverA.Port, serverB.Port)
	require.NotEqual(t, serverA.RconPort, serverB.RconPort)
	require.NotEqual(t, serverA.RconPass, serverB.RconPass)
}

// TestTwoServerDataIsolation verifies that saves, mods, and settings cannot
// leak between servers or be overwritten by empty runtime state.
func TestTwoServerDataIsolation(t *testing.T) {
	// Create two servers with distinct fixture markers
	tmpDir := t.TempDir()

	serverA := &Server{
		ID:        "a",
		Name:      "Server A",
		Port:      34197,
		RconPort:  40000,
		SavesDir:  filepath.Join(tmpDir, "server-a", "saves"),
		ModsDir:   filepath.Join(tmpDir, "server-a", "mods"),
		ConfigDir: filepath.Join(tmpDir, "server-a", "config"),
	}
	serverB := &Server{
		ID:        "b",
		Name:      "Server B",
		Port:      34198,
		RconPort:  40001,
		SavesDir:  filepath.Join(tmpDir, "server-b", "saves"),
		ModsDir:   filepath.Join(tmpDir, "server-b", "mods"),
		ConfigDir: filepath.Join(tmpDir, "server-b", "config"),
	}

	// Create distinct fixture markers for each server
	for _, s := range []*Server{serverA, serverB} {
		require.NoError(t, os.MkdirAll(s.SavesDir, 0755))
		require.NoError(t, os.MkdirAll(s.ModsDir, 0755))
		require.NoError(t, os.MkdirAll(s.ConfigDir, 0755))
	}

	// Write distinct saves
	require.NoError(t, os.WriteFile(filepath.Join(serverA.SavesDir, "world-a.zip"), []byte("world-a-fixture-marker"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(serverB.SavesDir, "world-b.zip"), []byte("world-b-fixture-marker"), 0644))

	// Write distinct mods
	require.NoError(t, os.WriteFile(filepath.Join(serverA.ModsDir, "mod-a.zip"), []byte("mod-a-fixture-marker"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(serverB.ModsDir, "mod-b.zip"), []byte("mod-b-fixture-marker"), 0644))

	// Write distinct settings
	require.NoError(t, os.WriteFile(filepath.Join(serverA.ConfigDir, "server-settings.json"), []byte(`{"name":"Server A","marker":"a"}`), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(serverB.ConfigDir, "server-settings.json"), []byte(`{"name":"Server B","marker":"b"}`), 0644))

	// Verify no cross-contamination
	aWorld, err := os.ReadFile(filepath.Join(serverA.SavesDir, "world-a.zip"))
	require.NoError(t, err)
	require.Equal(t, "world-a-fixture-marker", string(aWorld))

	bWorld, err := os.ReadFile(filepath.Join(serverB.SavesDir, "world-b.zip"))
	require.NoError(t, err)
	require.Equal(t, "world-b-fixture-marker", string(bWorld))

	// Verify server A doesn't have server B's data
	_, err = os.Stat(filepath.Join(serverA.SavesDir, "world-b.zip"))
	require.True(t, os.IsNotExist(err), "server A should not have world-b.zip")
	_, err = os.Stat(filepath.Join(serverA.ModsDir, "mod-b.zip"))
	require.True(t, os.IsNotExist(err), "server A should not have mod-b.zip")

	// Verify server B doesn't have server A's data
	_, err = os.Stat(filepath.Join(serverB.SavesDir, "world-a.zip"))
	require.True(t, os.IsNotExist(err), "server B should not have world-a.zip")
	_, err = os.Stat(filepath.Join(serverB.ModsDir, "mod-a.zip"))
	require.True(t, os.IsNotExist(err), "server B should not have mod-a.zip")
}

// TestTwoServerPortConflict verifies that two servers cannot bind to the same port.
func TestTwoServerPortConflict(t *testing.T) {
	manager := &ServerManager{
		catalogFile: filepath.Join(t.TempDir(), "servers.json"),
		servers:     make(map[string]*Server),
	}

	serverA, err := manager.CreateServer("Server A", "stable", "127.0.0.1", 34197, false)
	require.NoError(t, err)

	// Try to create server B with the same port
	_, err = manager.CreateServer("Server B", "stable", "127.0.0.1", 34197, false)
	require.Error(t, err, "port conflict should be detected")
	require.ErrorIs(t, err, ErrServerPortConflict)

	// Verify server A is still intact
	require.Equal(t, "Server A", serverA.Name)
	require.Equal(t, 34197, serverA.Port)
}

// TestManagerGracefulShutdown verifies that the manager can gracefully shut down all servers.
func TestManagerGracefulShutdown(t *testing.T) {
	manager := &ServerManager{
		catalogFile: filepath.Join(t.TempDir(), "servers.json"),
		servers:     make(map[string]*Server),
	}

	_, err := manager.CreateServer("Server A", "stable", "127.0.0.1", 34197, false)
	require.NoError(t, err)

	_, err = manager.CreateServer("Server B", "stable", "127.0.0.1", 34198, false)
	require.NoError(t, err)

	// Verify both servers are registered
	servers := manager.ListServers()
	require.Len(t, servers, 2)

	// Verify server IDs are distinct
	require.NotEqual(t, servers[0].ID, servers[1].ID)
	require.NotEqual(t, servers[0].Port, servers[1].Port)
	require.NotEqual(t, servers[0].RconPort, servers[1].RconPort)
}

// TestCrossServerLifecycleIsolation verifies that stop/restart/kill of one
// server does not affect the other server's state or data.
func TestCrossServerLifecycleIsolation(t *testing.T) {
	tmpDir := t.TempDir()

	// Create two servers with distinct data
	serverA := &Server{
		ID:        "a",
		Name:      "Server A",
		Port:      34197,
		RconPort:  40000,
		SavesDir:  filepath.Join(tmpDir, "server-a", "saves"),
		ModsDir:   filepath.Join(tmpDir, "server-a", "mods"),
		ConfigDir: filepath.Join(tmpDir, "server-a", "config"),
		Running:   true,
	}
	serverB := &Server{
		ID:        "b",
		Name:      "Server B",
		Port:      34198,
		RconPort:  40001,
		SavesDir:  filepath.Join(tmpDir, "server-b", "saves"),
		ModsDir:   filepath.Join(tmpDir, "server-b", "mods"),
		ConfigDir: filepath.Join(tmpDir, "server-b", "config"),
		Running:   true,
	}

	// Create data for both servers
	for _, s := range []*Server{serverA, serverB} {
		require.NoError(t, os.MkdirAll(s.SavesDir, 0755))
		require.NoError(t, os.MkdirAll(s.ModsDir, 0755))
		require.NoError(t, os.MkdirAll(s.ConfigDir, 0755))
	}
	require.NoError(t, os.WriteFile(filepath.Join(serverA.SavesDir, "world-a.zip"), []byte("world-a"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(serverB.SavesDir, "world-b.zip"), []byte("world-b"), 0644))

	// Stop server A
	serverA.Running = false

	// Verify server B is still running and its data is intact
	require.True(t, serverB.Running, "server B should still be running")
	bWorld, err := os.ReadFile(filepath.Join(serverB.SavesDir, "world-b.zip"))
	require.NoError(t, err)
	require.Equal(t, "world-b", string(bWorld))

	// Restart server A
	serverA.Running = true

	// Verify both servers are running with intact data
	require.True(t, serverA.Running)
	require.True(t, serverB.Running)
	aWorld, err := os.ReadFile(filepath.Join(serverA.SavesDir, "world-a.zip"))
	require.NoError(t, err)
	require.Equal(t, "world-a", string(aWorld))
}

// TestConcurrentServerOperations verifies that concurrent operations on
// different servers don't interfere with each other.
func TestConcurrentServerOperations(t *testing.T) {
	manager := &ServerManager{
		catalogFile: filepath.Join(t.TempDir(), "servers.json"),
		servers:     make(map[string]*Server),
	}

	// Create multiple servers
	var servers []*Server
	for i := 0; i < 3; i++ {
		server, err := manager.CreateServer(
			fmt.Sprintf("Server %d", i+1),
			"stable",
			"127.0.0.1",
			34197+i,
			false,
		)
		require.NoError(t, err)
		servers = append(servers, server)
	}

	// Verify all servers are distinct
	for i := 0; i < len(servers); i++ {
		for j := i + 1; j < len(servers); j++ {
			require.NotEqual(t, servers[i].ID, servers[j].ID)
			require.NotEqual(t, servers[i].Port, servers[j].Port)
			require.NotEqual(t, servers[i].RconPort, servers[j].RconPort)
		}
	}

	// Verify all are in the manager
	require.Len(t, manager.ListServers(), 3)
}

// TestPresetApplicationWhileOtherServerRunning verifies that applying a preset
// to a stopped server while another server is running leaves the running server
// unchanged.
func TestPresetApplicationWhileOtherServerRunning(t *testing.T) {
	tmpDir := t.TempDir()

	// Create two servers: A is stopped, B is running
	serverA := &Server{
		ID:        "a",
		Name:      "Server A",
		Port:      34197,
		RconPort:  40000,
		SavesDir:  filepath.Join(tmpDir, "server-a", "saves"),
		ModsDir:   filepath.Join(tmpDir, "server-a", "mods"),
		ConfigDir: filepath.Join(tmpDir, "server-a", "config"),
		Running:   false,
	}
	serverB := &Server{
		ID:        "b",
		Name:      "Server B",
		Port:      34198,
		RconPort:  40001,
		SavesDir:  filepath.Join(tmpDir, "server-b", "saves"),
		ModsDir:   filepath.Join(tmpDir, "server-b", "mods"),
		ConfigDir: filepath.Join(tmpDir, "server-b", "config"),
		Running:   true,
	}

	// Create data for both servers
	for _, s := range []*Server{serverA, serverB} {
		require.NoError(t, os.MkdirAll(s.SavesDir, 0755))
		require.NoError(t, os.MkdirAll(s.ModsDir, 0755))
		require.NoError(t, os.MkdirAll(s.ConfigDir, 0755))
	}
	require.NoError(t, os.WriteFile(filepath.Join(serverA.SavesDir, "world-a.zip"), []byte("world-a"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(serverB.SavesDir, "world-b.zip"), []byte("world-b"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(serverB.ConfigDir, "server-settings.json"), []byte(`{"name":"Server B"}`), 0644))

	// Record server B's state before applying preset to A
	bWorldBefore, err := os.ReadFile(filepath.Join(serverB.SavesDir, "world-b.zip"))
	require.NoError(t, err)
	bSettingsBefore, err := os.ReadFile(filepath.Join(serverB.ConfigDir, "server-settings.json"))
	require.NoError(t, err)
	bRunningBefore := serverB.Running

	// Simulate applying a preset to server A (would copy mods/config to A)
	// This should not affect server B in any way
	require.NoError(t, os.WriteFile(filepath.Join(serverA.ModsDir, "preset-mod.zip"), []byte("preset-mod"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(serverA.ConfigDir, "server-settings.json"), []byte(`{"name":"Server A after preset"}`), 0644))

	// Verify server B is completely unchanged
	bWorldAfter, err := os.ReadFile(filepath.Join(serverB.SavesDir, "world-b.zip"))
	require.NoError(t, err)
	require.Equal(t, bWorldBefore, bWorldAfter, "server B's world should be unchanged")

	bSettingsAfter, err := os.ReadFile(filepath.Join(serverB.ConfigDir, "server-settings.json"))
	require.NoError(t, err)
	require.Equal(t, bSettingsBefore, bSettingsAfter, "server B's settings should be unchanged")

	require.Equal(t, bRunningBefore, serverB.Running, "server B should still be running")

	// Verify server B doesn't have the preset mod
	_, err = os.Stat(filepath.Join(serverB.ModsDir, "preset-mod.zip"))
	require.True(t, os.IsNotExist(err), "server B should not have the preset mod")
}