package factorio

import (
	"path/filepath"
	"testing"
)

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

	// Delete server 1
	if err := manager.DeleteServer(server1.ID); err != nil {
		t.Fatalf("Failed to delete server 1: %v", err)
	}

	// Verify only server 2 remains
	servers := manager.ListServers()
	if len(servers) != 1 {
		t.Errorf("Expected 1 server after deletion, got %d", len(servers))
	}
	if servers[0].ID != server2.ID {
		t.Errorf("Expected remaining server to be %s, got %s", server2.ID, servers[0].ID)
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
