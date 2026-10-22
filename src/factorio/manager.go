package factorio

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
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

var ErrDefaultServer = errors.New("the default server cannot be deleted")

type ServerRecord struct {
	ID               string    `json:"id"`
	Name             string    `json:"name"`
	VersionLabel     string    `json:"version"`
	VersionChannel   string    `json:"version_channel"`
	Savefile         string    `json:"savefile"`
	BindIP           string    `json:"bind_ip"`
	Port             int       `json:"port"`
	RconPort         int       `json:"rcon_port"`
	RconPass         string    `json:"rcon_pass"`
	Autostart        bool      `json:"autostart"`
	WatchdogInterval int       `json:"watchdog_interval"`
	LastWatchdogPing time.Time `json:"-"`
	PendingRestart   bool      `json:"pending_restart"`
	ProfileID        string    `json:"profile_id"`
	UseMimalloc      bool      `json:"use_mimalloc"`
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
var serverManagerMu sync.RWMutex

func InitServerManager() (*ServerManager, error) {
	profileDataGate.RLock()
	defer profileDataGate.RUnlock()
	serverLifecycleMutex.Lock()
	defer serverLifecycleMutex.Unlock()
	if current := GetServerManager(); current != nil {
		for _, server := range current.ListServers() {
			if server.IsBusy() {
				return nil, ErrServerActive
			}
		}
	}
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
	if err := manager.rebuildServersLocked(); err != nil {
		return nil, err
	}

	serverManagerMu.Lock()
	serverManager = manager
	serverManagerMu.Unlock()
	go manager.rconWatchdog()
	return manager, nil
}

func GetServerManager() *ServerManager {
	serverManagerMu.RLock()
	defer serverManagerMu.RUnlock()
	return serverManager
}

func (m *ServerManager) loadCatalog() error {
	var candidate ServerCatalog
	if err := readBackupJSON(m.catalogFile, &candidate); err != nil {
		return err
	}
	if candidate.Version == 0 {
		candidate.Version = 1
	}
	if candidate.Version != 1 {
		return errors.New("unsupported server catalog version")
	}
	seen := make(map[string]bool)
	for _, record := range candidate.Servers {
		if ValidatePathElement(record.ID) != nil || seen[record.ID] {
			return errors.New("server catalog contains an invalid or duplicate ID")
		}
		seen[record.ID] = true
	}
	previous := m.catalog
	m.catalog = candidate
	for _, record := range candidate.Servers {
		if record.ID == DefaultServerID {
			return nil
		}
	}
	// Legacy controls always address this runtime, so it must have a catalog
	// identity even if an earlier version allowed deleting its record.
	m.catalog.Servers = append(m.catalog.Servers, legacyServerRecord(bootstrap.GetConfig()))
	if err := m.saveCatalogLocked(); err != nil {
		m.catalog = previous
		return err
	}
	return nil
}

func (m *ServerManager) saveCatalogLocked() error {
	raw, err := json.MarshalIndent(m.catalog, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomically(m.catalogFile, raw, 0600)
}

func (m *ServerManager) rebuildServers() (err error) {
	profileDataGate.RLock()
	defer profileDataGate.RUnlock()
	serverLifecycleMutex.Lock()
	defer serverLifecycleMutex.Unlock()
	return m.rebuildServersLocked()
}

func (m *ServerManager) rebuildServersLocked() (err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if GetFactorioServer().IsBusy() {
		return ErrServerActive
	}
	for _, server := range m.servers {
		if server.IsBusy() {
			return ErrServerActive
		}
	}
	installed := GetFactorioServer().ConfigurationSnapshot()
	var swaps []*directoryEntrySwap
	defer func() { finishManagedRuntimeSwaps(swaps, &err) }()

	next := make(map[string]*Server)
	var defaultRuntime *Server
	for _, record := range m.catalog.Servers {
		server, recordErr := serverFromRecord(record, filepath.Dir(m.catalogFile))
		if recordErr != nil || next[record.ID] != nil {
			return errors.New("server catalog contains an invalid or duplicate ID")
		}
		server.manager = m
		if record.ID == DefaultServerID {
			// Initialization has already loaded the active profile. Catalog
			// metadata must not replace its settings with an empty map.
			server.Settings = GetFactorioServer().SettingsSnapshot()
			server.Version = installed.Version
			server.BaseModVersion = installed.BaseModVersion
			if installed.Savefile != "" {
				server.Savefile = installed.Savefile
			}
			defaultRuntime = server
		} else {
			prepared, prepareErr := prepareManagedRuntime(server, installed)
			swaps = append(swaps, prepared...)
			if prepareErr != nil {
				return fmt.Errorf("initialize server %s: %w", record.ID, prepareErr)
			}
		}
		next[record.ID] = server
	}
	for _, swap := range swaps {
		if err := swap.activate(); err != nil {
			return err
		}
	}
	if defaultRuntime != nil {
		stateMutex.Lock()
		instantiated = *defaultRuntime
		stateMutex.Unlock()
		next[DefaultServerID] = GetFactorioServer()
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

func (m *ServerManager) CreateServer(name, versionLabel, bindIP string, port int, autostart bool) (server *Server, err error) {
	profileDataGate.RLock()
	defer profileDataGate.RUnlock()
	serverLifecycleMutex.Lock()
	defer serverLifecycleMutex.Unlock()
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
	name = strings.TrimSpace(name)
	if len(name) > 128 || strings.IndexFunc(name, func(character rune) bool { return character < 32 || character == 127 }) >= 0 ||
		net.ParseIP(bindIP) == nil || port < 1 || port > 65535 {
		return nil, fmt.Errorf("%w: invalid name, bind address or game port", ErrInvalidServerConfiguration)
	}
	for _, existing := range m.catalog.Servers {
		if existing.Port == port {
			return nil, ErrServerPortConflict
		}
	}
	if versionLabel == "" {
		versionLabel = "stable"
	}
	versionLabel, err = NormalizeReleaseTarget(versionLabel)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid version target", ErrInvalidServerConfiguration)
	}

	record := ServerRecord{
		ID:               id,
		Name:             name,
		VersionLabel:     versionLabel,
		VersionChannel:   versionLabel,
		BindIP:           bindIP,
		Port:             port,
		RconPort:         m.nextAvailableRconPortLocked(),
		RconPass:         bootstrap.GenerateRandomPassword(),
		Autostart:        autostart,
		WatchdogInterval: 30,
	}
	server, err = serverFromRecord(record, filepath.Dir(m.catalogFile))
	if err != nil {
		return nil, err
	}
	var swaps []*directoryEntrySwap
	createdRoot := ""
	defer func() {
		finishManagedRuntimeSwaps(swaps, &err)
		if err != nil && createdRoot != "" {
			for _, swap := range swaps {
				if swap.preserveRecovery {
					return
				}
			}
			_ = os.RemoveAll(createdRoot)
		}
	}()
	if record.ID != DefaultServerID {
		root := filepath.Dir(server.ConfigDir)
		parent := filepath.Dir(root)
		if err := os.Mkdir(parent, 0700); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		info, statErr := os.Lstat(parent)
		if statErr != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("%w: server storage parent must be a real directory", ErrInvalidServerConfiguration)
		}
		if err := os.Mkdir(root, 0700); err != nil {
			return nil, err
		}
		createdRoot = root
		installed := GetFactorioServer().ConfigurationSnapshot()
		if installed.Version != NilVersion {
			if err := validateManagedVersionTarget(installed.Version, versionLabel); err != nil {
				return nil, err
			}
		}
		swaps, err = prepareManagedRuntime(server, installed)
		if err != nil {
			return nil, err
		}
		for _, swap := range swaps {
			if err := swap.activate(); err != nil {
				return nil, err
			}
		}
	}
	previousCount := len(m.catalog.Servers)
	m.catalog.Servers = append(m.catalog.Servers, record)
	if err := m.saveCatalogLocked(); err != nil {
		m.catalog.Servers = m.catalog.Servers[:previousCount]
		return nil, err
	}
	server.manager = m
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
			m.catalog.Servers[i].UseMimalloc = server.UseMimalloc
			return m.saveCatalogLocked()
		}
	}
	return fmt.Errorf("server %s not found", server.ID)
}

func (m *ServerManager) DeleteServer(id string) error {
	if id == DefaultServerID {
		return ErrDefaultServer
	}
	serverLifecycleMutex.Lock()
	defer serverLifecycleMutex.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	if server, ok := m.servers[id]; ok && server.IsBusy() {
		return ErrServerActive
	}
	for i := range m.catalog.Servers {
		if m.catalog.Servers[i].ID == id {
			previous := m.catalog.Servers
			next := append([]ServerRecord(nil), previous[:i]...)
			next = append(next, previous[i+1:]...)
			m.catalog.Servers = next
			if err := m.saveCatalogLocked(); err != nil {
				m.catalog.Servers = previous
				return err
			}
			delete(m.servers, id)
			return nil
		}
	}
	return fmt.Errorf("server %s not found", id)
}

// Start routes a start request to the specified server instance.
func (m *ServerManager) Start(id, bindIP string, port int, savefile string) (<-chan error, error) {
	if id == "" {
		id = DefaultServerID
	}
	m.mu.RLock()
	server, ok := m.servers[id]
	m.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("server %s not found", id)
	}
	return server.Start(bindIP, port, savefile)
}

// Stop routes a stop request to the specified server instance.
func (m *ServerManager) Stop(id string) error {
	if id == "" {
		id = DefaultServerID
	}
	m.mu.RLock()
	server, ok := m.servers[id]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("server %s not found", id)
	}
	return server.Stop()
}

// Kill routes a kill request to the specified server instance.
func (m *ServerManager) Kill(id string) error {
	if id == "" {
		id = DefaultServerID
	}
	m.mu.RLock()
	server, ok := m.servers[id]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("server %s not found", id)
	}
	return server.Kill()
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
			if i != 1 {
				if _, err := os.Lstat(filepath.Join(filepath.Dir(m.catalogFile), "servers", strconv.Itoa(i))); err == nil {
					continue
				}
			}
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

func serverFromRecord(record ServerRecord, dataRoot string) (*Server, error) {
	if err := ValidatePathElement(record.ID); err != nil {
		return nil, errors.New("invalid server ID")
	}
	config := bootstrap.GetConfig()

	// Default server uses the existing global paths for backward compatibility.
	// Other servers get per-server directories under the data root.
	var savesDir, modsDir, configDir string
	if record.ID == DefaultServerID {
		savesDir = config.FactorioSavesDir
		modsDir = config.FactorioModsDir
		configDir = config.FactorioConfigDir
	} else {
		root, err := filepath.Abs(dataRoot)
		if err != nil {
			return nil, err
		}
		base := filepath.Join(root, "servers", record.ID)
		savesDir = filepath.Join(base, "saves")
		modsDir = filepath.Join(base, "mods")
		configDir = filepath.Join(base, "config")
	}

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
		UseMimalloc:      record.UseMimalloc,
		Settings:         make(map[string]interface{}),
		SavesDir:         savesDir,
		ModsDir:          modsDir,
		ConfigDir:        configDir,
	}, nil
}

func (m *ServerManager) migrateLegacy(config bootstrap.Config) error {
	log.Printf("No managed server catalog found; migrating legacy server into ID %s", DefaultServerID)
	m.catalog = ServerCatalog{Version: 1, Servers: []ServerRecord{legacyServerRecord(config)}}
	if err := m.saveCatalogLocked(); err != nil {
		return err
	}
	log.Printf("Legacy migration complete")
	return nil
}

func legacyServerRecord(config bootstrap.Config) ServerRecord {
	server := GetFactorioServer()
	versionLabel := defaultString(server.VersionLabel, "stable")
	port := server.Port
	if port == 0 {
		port = 34197
	}
	rconPort := config.FactorioRconPort
	if rconPort == 0 {
		rconPort = 40000
	}

	return ServerRecord{
		ID:               DefaultServerID,
		Name:             "Server 1",
		VersionLabel:     versionLabel,
		VersionChannel:   versionLabel,
		Savefile:         server.Savefile,
		BindIP:           defaultString(server.BindIP, defaultString(config.FactorioIP, "0.0.0.0")),
		Port:             port,
		RconPort:         rconPort,
		RconPass:         config.FactorioRconPass,
		Autostart:        config.Autostart == "true",
		WatchdogInterval: 30,
	}
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
	stateMutex.Lock()
	if server.WatchdogInterval <= 0 || !server.Running || server.Stopping ||
		time.Since(server.LastWatchdogPing) < time.Duration(server.WatchdogInterval)*time.Second {
		stateMutex.Unlock()
		return
	}
	server.LastWatchdogPing = time.Now()
	console, connected, id := server.Rcon, server.RconConnected, server.ID
	stateMutex.Unlock()
	if connected {
		var err error
		if console == nil {
			err = errors.New("RCON console is not ready")
		} else {
			_, err = server.RunPersistentRCONCommand("/players online count")
		}
		if err != nil {
			log.Printf("rconWatchdog: RCON lost for server %s: %v", id, err)
			stateMutex.Lock()
			current := server.Rcon == console
			if current {
				server.Rcon, server.RconConnected = nil, false
			}
			stateMutex.Unlock()
			if current && console != nil {
				_ = console.Close()
			}
		}
	}
	snapshot := server.Snapshot()
	if snapshot.Running && !snapshot.Stopping && !snapshot.RconConnected {
		if err := server.connectRC(); err == nil {
			log.Printf("rconWatchdog: RCON reconnected for server %s", id)
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
