package factorio

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/OpenFactorioServerManager/factorio-server-manager/bootstrap"
)

const DefaultServerID = "1"

type ServerRecord struct {
	ID             string        `json:"id"`
	Name           string        `json:"name"`
	VersionLabel   string        `json:"version"`
	VersionChannel string        `json:"version_channel"`
	Savefile       string        `json:"savefile"`
	BindIP         string        `json:"bind_ip"`
	Port           int           `json:"port"`
	RconPort       int           `json:"rcon_port"`
	RconPass       string        `json:"rcon_pass"`
	Autostart      bool          `json:"autostart"`
	WatchdogInterval int         `json:"watchdog_interval"`
	LastWatchdogPing time.Time   `json:"-"`
	PendingRestart bool          `json:"pending_restart"`
	ProfileID      string        `json:"profile_id"`
}

type ServerCatalog struct {
	Version int            `json:"version"`
	Servers []ServerRecord `json:"servers"`
}

type ServerManager struct {
	mu          sync.RWMutex
	catalogFile string
	catalog     ServerCatalog
	servers     map[string]*Server
}

var serverManager *ServerManager

func InitServerManager() (*ServerManager, error) {
	config := bootstrap.GetConfig()
	manager := &ServerManager{
		catalogFile: filepath.Join(filepath.Dir(config.ConfFile), "servers.json"),
		servers:     make(map[string]*Server),
	}

	if _, err := os.Stat(manager.catalogFile); os.IsNotExist(err) {
		if err := manager.migrateLegacy(config); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}

	if err := manager.loadCatalog(); err != nil {
		return nil, err
	}
	if err := manager.rebuildServers(); err != nil {
		return nil, err
	}

	serverManager = manager
	SetFactorioServer(*manager.DefaultServer())
	go manager.rconWatchdog()
	return manager, nil
}

func GetServerManager() *ServerManager {
	return serverManager
}

func (m *ServerManager) loadCatalog() error {
	raw, err := os.ReadFile(m.catalogFile)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, &m.catalog); err != nil {
		return err
	}
	if m.catalog.Version == 0 {
		m.catalog.Version = 1
	}
	return nil
}

func (m *ServerManager) saveCatalogLocked() error {
	tmp := m.catalogFile + ".tmp"
	raw, err := json.MarshalIndent(m.catalog, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(tmp, raw, 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, m.catalogFile); err != nil {
		_ = os.Remove(m.catalogFile)
		return os.Rename(tmp, m.catalogFile)
	}
	return nil
}

func (m *ServerManager) rebuildServers() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	next := make(map[string]*Server)
	for _, record := range m.catalog.Servers {
		server := serverFromRecord(record)
		next[record.ID] = server
	}
	m.servers = next
	return nil
}

func (m *ServerManager) ListServers() []*Server {
	m.mu.RLock()
	defer m.mu.RUnlock()

	ids := make([]string, 0, len(m.servers))
	for id := range m.servers {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		ii, _ := strconv.Atoi(ids[i])
		jj, _ := strconv.Atoi(ids[j])
		if ii == 0 || jj == 0 {
			return ids[i] < ids[j]
		}
		return ii < jj
	})

	result := make([]*Server, 0, len(ids))
	for _, id := range ids {
		result = append(result, m.servers[id])
	}
	return result
}

func (m *ServerManager) GetServer(id string) (*Server, bool) {
	if id == "" {
		id = DefaultServerID
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	server, ok := m.servers[id]
	return server, ok
}

func (m *ServerManager) DefaultServer() *Server {
	server, ok := m.GetServer(DefaultServerID)
	if ok {
		return server
	}
	return &instantiated
}

func (m *ServerManager) CreateServer(name, versionLabel, bindIP string, port int, autostart bool) (*Server, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	id := m.nextAvailableServerIDLocked()
	if strings.TrimSpace(name) == "" {
		name = fmt.Sprintf("Server %s", id)
	}
	if bindIP == "" {
		bindIP = "0.0.0.0"
	}
	if port == 0 {
		port = m.nextAvailableGamePortLocked()
	}

	record := ServerRecord{
		ID:             id,
		Name:           name,
		VersionLabel:   versionLabel,
		VersionChannel: versionLabel,
		BindIP:         bindIP,
		Port:           port,
		RconPort:       m.nextAvailableRconPortLocked(),
		RconPass:       bootstrap.GenerateRandomPassword(),
		Autostart:      autostart,
		WatchdogInterval: 30,
	}
	m.catalog.Servers = append(m.catalog.Servers, record)
	if err := m.saveCatalogLocked(); err != nil {
		return nil, err
	}

	server := serverFromRecord(record)
	m.servers[id] = server
	return server, nil
}

func (m *ServerManager) UpdateServer(server *Server) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.catalog.Servers {
		if m.catalog.Servers[i].ID == server.ID {
			m.catalog.Servers[i].Name = server.Name
			m.catalog.Servers[i].BindIP = server.BindIP
			m.catalog.Servers[i].Port = server.Port
			m.catalog.Servers[i].RconPort = server.RconPort
			m.catalog.Servers[i].RconPass = server.RconPass
			m.catalog.Servers[i].Autostart = server.Autostart
			m.catalog.Servers[i].WatchdogInterval = server.WatchdogInterval
			m.catalog.Servers[i].Savefile = server.Savefile
			m.catalog.Servers[i].PendingRestart = server.PendingRestart
			m.catalog.Servers[i].VersionLabel = server.VersionLabel
			m.catalog.Servers[i].VersionChannel = server.VersionChannel
			m.catalog.Servers[i].ProfileID = server.ProfileID
			return m.saveCatalogLocked()
		}
	}
	return fmt.Errorf("server %s not found", server.ID)
}

func (m *ServerManager) DeleteServer(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.catalog.Servers {
		if m.catalog.Servers[i].ID == id {
			m.catalog.Servers = append(m.catalog.Servers[:i], m.catalog.Servers[i+1:]...)
			delete(m.servers, id)
			return m.saveCatalogLocked()
		}
	}
	return fmt.Errorf("server %s not found", id)
}

func (m *ServerManager) nextAvailableServerIDLocked() string {
	used := make(map[int]bool)
	for _, srv := range m.catalog.Servers {
		if n, err := strconv.Atoi(srv.ID); err == nil {
			used[n] = true
		}
	}
	for i := 1; ; i++ {
		if !used[i] {
			return strconv.Itoa(i)
		}
	}
}

func (m *ServerManager) PreviewNextServer() (id string, name string, port int) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	id = m.nextAvailableServerIDLocked()
	name = fmt.Sprintf("Server %s", id)
	port = m.nextAvailableGamePortLocked()
	return id, name, port
}

func (m *ServerManager) nextAvailableGamePortLocked() int {
	start := 34197
	end := 34220
	used := make(map[int]bool)
	for _, srv := range m.catalog.Servers {
		used[srv.Port] = true
	}
	for port := start; port <= end; port++ {
		if !used[port] {
			return port
		}
	}
	return end + 1
}

func (m *ServerManager) nextAvailableRconPortLocked() int {
	used := make(map[int]bool)
	for _, srv := range m.catalog.Servers {
		used[srv.RconPort] = true
	}
	for port := 40000; port <= 45000; port++ {
		if !used[port] {
			return port
		}
	}
	return 45001
}

func serverFromRecord(record ServerRecord) *Server {
	return &Server{
		ID:               record.ID,
		Name:             record.Name,
		VersionLabel:     record.VersionLabel,
		VersionChannel:   record.VersionChannel,
		Savefile:         record.Savefile,
		BindIP:           defaultString(record.BindIP, "0.0.0.0"),
		Port:             record.Port,
		RconPort:         record.RconPort,
		RconPass:         record.RconPass,
		Autostart:        record.Autostart,
		WatchdogInterval: record.WatchdogInterval,
		LastWatchdogPing: record.LastWatchdogPing,
		PendingRestart:   record.PendingRestart,
		ProfileID:        record.ProfileID,
		Settings:         make(map[string]interface{}),
	}
}

func (m *ServerManager) migrateLegacy(config bootstrap.Config) error {
	log.Printf("No managed server catalog found; migrating legacy server into ID %s", DefaultServerID)

	versionLabel := "stable"
	if server := GetFactorioServer(); server != nil && server.VersionLabel != "" {
		versionLabel = server.VersionLabel
	}

	port := 34197
	rconPort := config.FactorioRconPort
	if rconPort == 0 {
		rconPort = 40000
	}

	m.catalog = ServerCatalog{
		Version: 1,
		Servers: []ServerRecord{
			{
				ID:             DefaultServerID,
				Name:           "Server 1",
				VersionLabel:   versionLabel,
				VersionChannel: versionLabel,
				BindIP:         defaultString(config.FactorioIP, "0.0.0.0"),
				Port:           port,
				RconPort:       rconPort,
				RconPass:       config.FactorioRconPass,
				Autostart:      config.Autostart == "true",
				WatchdogInterval: 30,
			},
		},
	}

	if err := m.saveCatalogLocked(); err != nil {
		return err
	}

	log.Printf("Legacy migration complete")
	return nil
}

func (m *ServerManager) rconWatchdog() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		for _, server := range m.ListServers() {
			m.watchdogCheckServer(server)
		}
	}
}

func (m *ServerManager) watchdogCheckServer(server *Server) {
	if server.WatchdogInterval <= 0 {
		return
	}
	if !server.GetRunning() {
		return
	}
	if time.Since(server.LastWatchdogPing) < time.Duration(server.WatchdogInterval)*time.Second {
		return
	}
	server.LastWatchdogPing = time.Now()
	if server.RconConnected {
		if server.Rcon == nil {
			server.RconConnected = false
		} else if _, err := server.Rcon.Write("/players online count"); err != nil {
			log.Printf("rconWatchdog: RCON lost for server %s: %s", server.ID, err)
			server.RconConnected = false
		}
	}
	if !server.RconConnected {
		if err := server.connectRC(); err == nil {
			server.RconConnected = true
			log.Printf("rconWatchdog: RCON reconnected for server %s", server.ID)
		}
	}
}

func parsePortRange(value string) (int, int) {
	parts := strings.SplitN(value, "-", 2)
	start := 34197
	end := 34220
	if len(parts) > 0 {
		if parsed, err := strconv.Atoi(strings.TrimSpace(parts[0])); err == nil {
			start = parsed
		}
	}
	if len(parts) > 1 {
		if parsed, err := strconv.Atoi(strings.TrimSpace(parts[1])); err == nil {
			end = parsed
		}
	}
	if end < start {
		end = start
	}
	return start, end
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
