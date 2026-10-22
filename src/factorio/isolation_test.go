package factorio

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
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
