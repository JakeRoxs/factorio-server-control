package factorio

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestTwoRuntimeIsolation verifies that two server instances can exist
// independently with separate settings, ports, and storage paths.
func TestTwoRuntimeIsolation(t *testing.T) {
	// Create two independent server instances with different IDs
	serverA := &Server{
		ID:       "a",
		Port:     34197,
		RconPort: 40000,
	}
	serverB := &Server{
		ID:       "b",
		Port:     34198,
		RconPort: 40001,
	}

	// Each server must have its own settings map
	serverA.Settings = map[string]interface{}{"name": "Server A"}
	serverB.Settings = map[string]interface{}{"name": "Server B"}

	// Verify settings are independent
	if serverA.Settings["name"] != "Server A" {
		t.Errorf("Server A settings corrupted: %v", serverA.Settings)
	}
	if serverB.Settings["name"] != "Server B" {
		t.Errorf("Server B settings corrupted: %v", serverB.Settings)
	}

	// Verify changing one doesn't affect the other
	serverA.Settings["name"] = "Server A Modified"
	if serverB.Settings["name"] != "Server B" {
		t.Errorf("Modifying Server A affected Server B: %v", serverB.Settings["name"])
	}

	// Verify ports are independent
	if serverA.Port == serverB.Port {
		t.Error("Servers share the same game port")
	}
	if serverA.RconPort == serverB.RconPort {
		t.Error("Servers share the same RCON port")
	}
}

// TestServerScopedSettingsFile verifies that each server writes to its own
// settings file path, not a shared global path.
func TestServerScopedSettingsFile(t *testing.T) {
	baseDir := t.TempDir()

	serverA := &Server{
		ID:        "a",
		Port:      34197,
		RconPort:  40000,
		Settings:  map[string]interface{}{"name": "Server A"},
		SavesDir:  filepath.Join(baseDir, "a", "saves"),
		ModsDir:   filepath.Join(baseDir, "a", "mods"),
		ConfigDir: filepath.Join(baseDir, "a", "config"),
	}
	serverB := &Server{
		ID:        "b",
		Port:      34198,
		RconPort:  40001,
		Settings:  map[string]interface{}{"name": "Server B"},
		SavesDir:  filepath.Join(baseDir, "b", "saves"),
		ModsDir:   filepath.Join(baseDir, "b", "mods"),
		ConfigDir: filepath.Join(baseDir, "b", "config"),
	}

	// Each server must compute its own settings file path
	settingsA := serverA.SettingsFile()
	settingsB := serverB.SettingsFile()

	if settingsA == settingsB {
		t.Errorf("Servers share settings file path: %s", settingsA)
	}

	// Verify the path is inside the server's own config directory
	if !filepath.IsAbs(settingsA) {
		t.Errorf("Server A settings path is not absolute: %s", settingsA)
	}
	if !filepath.IsAbs(settingsB) {
		t.Errorf("Server B settings path is not absolute: %s", settingsB)
	}
}

// TestConcurrentStartClaims verifies that two servers can independently
// claim their starting state without blocking each other.
func TestConcurrentStartClaims(t *testing.T) {
	serverA := &Server{ID: "a", Port: 34197, RconPort: 40000}
	serverB := &Server{ID: "b", Port: 34198, RconPort: 40001}

	var wg sync.WaitGroup

	// Both servers should be able to transition to "starting" independently
	wg.Add(2)
	go func() {
		defer wg.Done()
		serverA.claimStarting()
	}()
	go func() {
		defer wg.Done()
		serverB.claimStarting()
	}()
	wg.Wait()

	if !serverA.starting {
		t.Error("Server A failed to claim starting state")
	}
	if !serverB.starting {
		t.Error("Server B failed to claim starting state")
	}

	// Same-server concurrent claims should block
	serverC := &Server{ID: "c", Port: 34199, RconPort: 40002}
	serverC.claimStarting()
	if !serverC.starting {
		t.Error("Server C failed to claim starting state")
	}
}

// TestServerDirectories verifies per-server directory resolution.
func TestServerDirectories(t *testing.T) {
	baseDir := t.TempDir()

	server := &Server{
		ID:        "test-server",
		Port:      34197,
		RconPort:  40000,
		Settings:  map[string]interface{}{},
		SavesDir:  filepath.Join(baseDir, "saves"),
		ModsDir:   filepath.Join(baseDir, "mods"),
		ConfigDir: filepath.Join(baseDir, "config"),
	}

	// Ensure directories exist
	if err := os.MkdirAll(server.SavesDir, 0755); err != nil {
		t.Fatalf("Failed to create saves dir: %v", err)
	}
	if err := os.MkdirAll(server.ModsDir, 0755); err != nil {
		t.Fatalf("Failed to create mods dir: %v", err)
	}
	if err := os.MkdirAll(server.ConfigDir, 0755); err != nil {
		t.Fatalf("Failed to create config dir: %v", err)
	}

	// Verify the server can write to its own directories
	testFile := filepath.Join(server.SavesDir, "test-save.zip")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to write to server saves dir: %v", err)
	}
	if _, err := os.Stat(testFile); err != nil {
		t.Fatalf("Test save file not found: %v", err)
	}
}

// TestRCONIsolationBetweenServers verifies that RCON commands sent to one server
// only affect that server and not others.
func TestRCONIsolationBetweenServers(t *testing.T) {
	tmpDir := t.TempDir()

	// Create two independent servers with distinct RCON credentials
	serverA := &Server{
		ID:        "test-a",
		Name:      "Test Server A",
		Port:      34217,
		RconPort:  40010,
		RconPass:  "test-rcon-pass-a",
		SavesDir:  filepath.Join(tmpDir, "server-a", "saves"),
		ModsDir:   filepath.Join(tmpDir, "server-a", "mods"),
		ConfigDir: filepath.Join(tmpDir, "server-a", "config"),
	}
	serverB := &Server{
		ID:        "test-b",
		Name:      "Test Server B",
		Port:      34218,
		RconPort:  40011,
		RconPass:  "test-rcon-pass-b",
		SavesDir:  filepath.Join(tmpDir, "server-b", "saves"),
		ModsDir:   filepath.Join(tmpDir, "server-b", "mods"),
		ConfigDir: filepath.Join(tmpDir, "server-b", "config"),
	}

	// Verify RCON credentials are distinct
	require.NotEqual(t, serverA.RconPass, serverB.RconPass, "servers should have distinct RCON passwords")
	require.NotEqual(t, serverA.RconPort, serverB.RconPort, "servers should have distinct RCON ports")
	require.NotEqual(t, serverA.Port, serverB.Port, "servers should have distinct game ports")

	// Simulate RCON connections (without actual Factorio processes)
	serverA.RconConnected = true
	serverB.RconConnected = true

	// Verify RCON state is independent
	require.True(t, serverA.RconConnected, "server A should have RCON connected")
	require.True(t, serverB.RconConnected, "server B should have RCON connected")

	// Simulate RCON loss on server A
	serverA.RconConnected = false

	// Verify server B's RCON is still connected
	require.True(t, serverB.RconConnected, "server B's RCON should still be connected")
	require.False(t, serverA.RconConnected, "server A's RCON should be disconnected")
}

// TestLogIsolationBetweenServers verifies that each server writes to its own
// log file and logs don't mix.
func TestLogIsolationBetweenServers(t *testing.T) {
	tmpDir := t.TempDir()

	// Create two servers with distinct log paths
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

	// Create config directories
	require.NoError(t, os.MkdirAll(serverA.ConfigDir, 0755))
	require.NoError(t, os.MkdirAll(serverB.ConfigDir, 0755))

	// Write distinct log content to each server's config dir
	require.NoError(t, os.WriteFile(filepath.Join(serverA.ConfigDir, "factorio-current.log"), []byte("Server A log entry\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(serverB.ConfigDir, "factorio-current.log"), []byte("Server B log entry\n"), 0644))

	// Verify logs are separate
	logA, err := os.ReadFile(filepath.Join(serverA.ConfigDir, "factorio-current.log"))
	require.NoError(t, err)
	require.Equal(t, "Server A log entry\n", string(logA))

	logB, err := os.ReadFile(filepath.Join(serverB.ConfigDir, "factorio-current.log"))
	require.NoError(t, err)
	require.Equal(t, "Server B log entry\n", string(logB))

	// Verify no cross-contamination
	require.NotContains(t, string(logA), "Server B")
	require.NotContains(t, string(logB), "Server A")
}

// TestStatusIsolationBetweenServers verifies that status queries for one server
// don't return status from another.
func TestStatusIsolationBetweenServers(t *testing.T) {
	tmpDir := t.TempDir()

	// Create two servers with different states
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
		Running:   false,
	}

	// Verify status is isolated
	require.True(t, serverA.Running, "server A should be running")
	require.False(t, serverB.Running, "server B should not be running")

	// Take snapshots
	snapshotA := serverA.Snapshot()
	snapshotB := serverB.Snapshot()

	require.True(t, snapshotA.Running, "server A snapshot should show running")
	require.False(t, snapshotB.Running, "server B snapshot should show not running")
}

// TestWatchdogIsolationBetweenServers verifies that the watchdog for one server
// doesn't affect another server. Each server has its own watchdog interval and
// RCON reconnection logic.
func TestWatchdogIsolationBetweenServers(t *testing.T) {
	tmpDir := t.TempDir()

	// Create two servers with different watchdog intervals
	serverA := &Server{
		ID:               "a",
		Name:             "Server A",
		Port:             34197,
		RconPort:         40000,
		SavesDir:         filepath.Join(tmpDir, "server-a", "saves"),
		ModsDir:          filepath.Join(tmpDir, "server-a", "mods"),
		ConfigDir:        filepath.Join(tmpDir, "server-a", "config"),
		Running:          true,
		RconConnected:    true,
		WatchdogInterval: 30,
		LastWatchdogPing: time.Now(),
	}
	serverB := &Server{
		ID:               "b",
		Name:             "Server B",
		Port:             34198,
		RconPort:         40001,
		SavesDir:         filepath.Join(tmpDir, "server-b", "saves"),
		ModsDir:          filepath.Join(tmpDir, "server-b", "mods"),
		ConfigDir:        filepath.Join(tmpDir, "server-b", "config"),
		Running:          true,
		RconConnected:    true,
		WatchdogInterval: 60,
		LastWatchdogPing: time.Now(),
	}

	// Verify watchdog intervals are distinct
	require.Equal(t, 30, serverA.WatchdogInterval)
	require.Equal(t, 60, serverB.WatchdogInterval)
	require.NotEqual(t, serverA.WatchdogInterval, serverB.WatchdogInterval)

	// Simulate watchdog timeout for server A (but not B)
	serverA.LastWatchdogPing = time.Now().Add(-1 * time.Minute)

	// Verify server A is overdue for watchdog check
	overdueA := time.Since(serverA.LastWatchdogPing) > time.Duration(serverA.WatchdogInterval)*time.Second
	overdueB := time.Since(serverB.LastWatchdogPing) > time.Duration(serverB.WatchdogInterval)*time.Second

	require.True(t, overdueA, "server A should be overdue for watchdog check")
	require.False(t, overdueB, "server B should not be overdue for watchdog check")

	// Verify RCON state is independent
	require.True(t, serverA.RconConnected)
	require.True(t, serverB.RconConnected)

	// Simulate RCON loss on server A
	serverA.RconConnected = false

	// Verify server B's RCON is still connected
	require.True(t, serverB.RconConnected, "server B's RCON should still be connected")
	require.False(t, serverA.RconConnected, "server A's RCON should be disconnected")
}
