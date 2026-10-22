package factorio

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCatalogActivationFailurePreservesPreviousFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "servers.json")
	previous := []byte(`{"version":1,"servers":[{"id":"1","name":"Keep"}]}`)
	if err := os.WriteFile(path, previous, 0600); err != nil {
		t.Fatal(err)
	}
	originalRename := atomicFileRename
	t.Cleanup(func() { atomicFileRename = originalRename })
	atomicFileRename = func(source, destination string) error {
		if destination == path && strings.HasPrefix(filepath.Base(source), ".fsm-write-") {
			return errors.New("synthetic catalog activation failure")
		}
		return os.Rename(source, destination)
	}
	manager := &ServerManager{catalogFile: path, catalog: ServerCatalog{Version: 1}}
	if err := manager.saveCatalogLocked(); err == nil {
		t.Fatal("catalog ignored failed atomic activation")
	}
	actual, err := os.ReadFile(path)
	if err != nil || string(actual) != string(previous) {
		t.Fatalf("previous catalog was not preserved: %v", err)
	}
}

func TestServerRecordRestoresMimalloc(t *testing.T) {
	server, err := serverFromRecord(ServerRecord{ID: "2", UseMimalloc: true}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !server.UseMimalloc {
		t.Fatal("saved mimalloc option was discarded")
	}
}

func TestServerConfigurationPersistsAllocatorWhileRunning(t *testing.T) {
	manager := &ServerManager{catalogFile: filepath.Join(t.TempDir(), "servers.json"), servers: make(map[string]*Server)}
	server, err := manager.CreateServer("Running game", "stable", "127.0.0.1", 34197, false)
	if err != nil {
		t.Fatal(err)
	}
	server.Running = true
	enabled := true
	updated, err := manager.UpdateServerConfiguration(server.ID, ServerConfigurationUpdate{UseMimalloc: &enabled})
	if err != nil || !updated.UseMimalloc || !server.Running {
		t.Fatalf("allocator preference was not saved without stopping the game: %v", err)
	}
	loaded := &ServerManager{catalogFile: manager.catalogFile}
	if err := loaded.loadCatalog(); err != nil {
		t.Fatal(err)
	}
	if !loaded.catalog.Servers[0].UseMimalloc {
		t.Fatal("allocator preference was not persisted")
	}
	otherPort := 34200
	if _, err := manager.UpdateServerConfiguration(server.ID, ServerConfigurationUpdate{Port: &otherPort}); !errors.Is(err, ErrServerActive) {
		t.Fatalf("running-server port change was not rejected: %v", err)
	}
	if server.Port != 34197 {
		t.Fatal("rejected request changed live network configuration")
	}
}

func TestServerConfigurationFailurePreservesRuntimeAndCatalog(t *testing.T) {
	manager := &ServerManager{catalogFile: filepath.Join(t.TempDir(), "servers.json"), servers: make(map[string]*Server)}
	server, err := manager.CreateServer("Original", "stable", "127.0.0.1", 34197, false)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(manager.catalogFile)
	if err != nil {
		t.Fatal(err)
	}
	originalRename := atomicFileRename
	t.Cleanup(func() { atomicFileRename = originalRename })
	atomicFileRename = func(source, destination string) error {
		if destination == manager.catalogFile && strings.HasPrefix(filepath.Base(source), ".fsm-write-") {
			return errors.New("synthetic configuration persistence failure")
		}
		return os.Rename(source, destination)
	}
	enabled := true
	if _, err := manager.UpdateServerConfiguration(server.ID, ServerConfigurationUpdate{UseMimalloc: &enabled}); err == nil {
		t.Fatal("failed persistence was reported as success")
	}
	if server.UseMimalloc || manager.catalog.Servers[0].UseMimalloc {
		t.Fatal("failed persistence changed runtime or in-memory catalog preference")
	}
	after, err := os.ReadFile(manager.catalogFile)
	if err != nil || string(before) != string(after) {
		t.Fatal("failed persistence changed the prior catalog file")
	}
}

func TestServerManagerCannotDeleteDefaultRuntime(t *testing.T) {
	manager := &ServerManager{catalogFile: filepath.Join(t.TempDir(), "servers.json"), servers: make(map[string]*Server)}
	server, err := manager.CreateServer("Existing server", "stable", "0.0.0.0", 34197, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.DeleteServer(server.ID); err == nil {
		t.Fatal("default runtime can be orphaned by deleting its catalog entry")
	}
}

func TestLoadCatalogRestoresMissingDefaultServer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "servers.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"servers":[{"id":"2","name":"Keep this server"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	manager := &ServerManager{catalogFile: path}
	if err := manager.loadCatalog(); err != nil {
		t.Fatal(err)
	}
	if len(manager.catalog.Servers) != 2 {
		t.Fatalf("expected preserved server and restored default, got %v", manager.catalog.Servers)
	}
	if manager.catalog.Servers[0].ID != "2" || manager.catalog.Servers[1].ID != DefaultServerID {
		t.Fatal("existing catalog entries were changed or default was not restored")
	}
}

func TestRebuildServersPreservesLoadedDefaultRuntime(t *testing.T) {
	original := *GetFactorioServer()
	t.Cleanup(func() { SetFactorioServer(original) })
	SetFactorioServer(Server{
		Settings:       map[string]interface{}{"name": "Existing game", "description": "Keep my settings"},
		Version:        Version{2, 1, 22},
		BaseModVersion: "2.1.22",
		Savefile:       "existing-save.zip",
	})
	manager := &ServerManager{catalog: ServerCatalog{Servers: []ServerRecord{{
		ID: DefaultServerID, Name: "Server 1", Port: 34197, Savefile: "stale-catalog.zip",
	}}}}
	if err := manager.rebuildServers(); err != nil {
		t.Fatal(err)
	}
	server := manager.DefaultServer()
	if server.Settings["name"] != "Existing game" || server.Settings["description"] != "Keep my settings" {
		t.Fatalf("catalog discarded loaded settings: %v", server.Settings)
	}
	if server.Version != (Version{2, 1, 22}) || server.BaseModVersion != "2.1.22" || server.Savefile != "existing-save.zip" {
		t.Fatal("catalog discarded loaded version or selected save")
	}
	if server != GetFactorioServer() {
		t.Fatal("catalog and legacy controls use different default server instances")
	}
}

func TestServerManagerCreateServer(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir := t.TempDir()

	// Create a ServerManager with a temporary catalog file
	manager := &ServerManager{
		catalogFile: filepath.Join(tmpDir, "servers.json"),
		servers:     make(map[string]*Server),
	}

	// Create a server
	server, err := manager.CreateServer("Test Server", "stable", "0.0.0.0", 34197, false)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	if server.ID != "1" {
		t.Errorf("Expected server ID 1, got %s", server.ID)
	}
	if server.Name != "Test Server" {
		t.Errorf("Expected server name 'Test Server', got %s", server.Name)
	}
	if server.Port != 34197 {
		t.Errorf("Expected server port 34197, got %d", server.Port)
	}

	// Verify the server was added to the catalog
	servers := manager.ListServers()
	if len(servers) != 1 {
		t.Errorf("Expected 1 server in catalog, got %d", len(servers))
	}
}

func TestServerManagerUpdateServer(t *testing.T) {
	tmpDir := t.TempDir()
	manager := &ServerManager{
		catalogFile: filepath.Join(tmpDir, "servers.json"),
		servers:     make(map[string]*Server),
	}

	// Create a server
	server, err := manager.CreateServer("Test Server", "stable", "0.0.0.0", 34197, false)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Update the server
	server.Name = "Updated Server"
	server.Port = 34200
	if err := manager.UpdateServer(server); err != nil {
		t.Fatalf("Failed to update server: %v", err)
	}

	// Verify the update
	updated, ok := manager.GetServer("1")
	if !ok {
		t.Fatal("Failed to get updated server")
	}
	if updated.Name != "Updated Server" {
		t.Errorf("Expected updated name 'Updated Server', got %s", updated.Name)
	}
	if updated.Port != 34200 {
		t.Errorf("Expected updated port 34200, got %d", updated.Port)
	}
}

func TestServerManagerDeleteServer(t *testing.T) {
	tmpDir := t.TempDir()
	manager := &ServerManager{
		catalogFile: filepath.Join(tmpDir, "servers.json"),
		servers:     make(map[string]*Server),
	}

	// Create two servers
	server1, err := manager.CreateServer("Server 1", "stable", "0.0.0.0", 34197, false)
	if err != nil {
		t.Fatalf("Failed to create server 1: %v", err)
	}
	server2, err := manager.CreateServer("Server 2", "stable", "0.0.0.0", 34198, false)
	if err != nil {
		t.Fatalf("Failed to create server 2: %v", err)
	}

	// Delete the additional server, preserving the default runtime identity.
	if err := manager.DeleteServer(server2.ID); err != nil {
		t.Fatalf("Failed to delete server 1: %v", err)
	}

	// Verify only server 2 remains
	servers := manager.ListServers()
	if len(servers) != 1 {
		t.Errorf("Expected 1 server after deletion, got %d", len(servers))
	}
	if servers[0].ID != server1.ID {
		t.Errorf("Expected remaining server to be %s, got %s", server1.ID, servers[0].ID)
	}
}

func TestServerManagerPortAllocation(t *testing.T) {
	tmpDir := t.TempDir()
	manager := &ServerManager{
		catalogFile: filepath.Join(tmpDir, "servers.json"),
		servers:     make(map[string]*Server),
	}

	// Create three servers without specifying ports
	_, err := manager.CreateServer("Server 1", "stable", "0.0.0.0", 0, false)
	if err != nil {
		t.Fatalf("Failed to create server 1: %v", err)
	}
	_, err = manager.CreateServer("Server 2", "stable", "0.0.0.0", 0, false)
	if err != nil {
		t.Fatalf("Failed to create server 2: %v", err)
	}
	_, err = manager.CreateServer("Server 3", "stable", "0.0.0.0", 0, false)
	if err != nil {
		t.Fatalf("Failed to create server 3: %v", err)
	}

	// Verify ports are allocated sequentially
	servers := manager.ListServers()
	if len(servers) != 3 {
		t.Errorf("Expected 3 servers, got %d", len(servers))
	}
	if servers[0].Port != 34197 {
		t.Errorf("Expected server 1 port 34197, got %d", servers[0].Port)
	}
	if servers[1].Port != 34198 {
		t.Errorf("Expected server 2 port 34198, got %d", servers[1].Port)
	}
	if servers[2].Port != 34199 {
		t.Errorf("Expected server 3 port 34199, got %d", servers[2].Port)
	}
}

func TestServerManagerGetDefaultServer(t *testing.T) {
	tmpDir := t.TempDir()
	manager := &ServerManager{
		catalogFile: filepath.Join(tmpDir, "servers.json"),
		servers:     make(map[string]*Server),
	}

	// Create a server
	server, err := manager.CreateServer("Test Server", "stable", "0.0.0.0", 34197, false)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Verify default server is the first server
	defaultServer := manager.DefaultServer()
	if defaultServer.ID != server.ID {
		t.Errorf("Expected default server to be %s, got %s", server.ID, defaultServer.ID)
	}
}
