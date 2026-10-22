package factorio

import (
	"errors"
	"fmt"
	"net"
	"strings"
)

var ErrInvalidServerConfiguration = errors.New("invalid server configuration")
var ErrServerPortConflict = errors.New("server game port is already in use")

type ServerConfigurationUpdate struct {
	Name             *string `json:"name"`
	BindIP           *string `json:"bind_ip"`
	Port             *int    `json:"port"`
	Autostart        *bool   `json:"autostart"`
	WatchdogInterval *int    `json:"watchdog_interval"`
	UseMimalloc      *bool   `json:"use_mimalloc"`
}

// Allocator selection is a next-start preference, not a live process mutation.
// Publish the candidate only after the catalog has been persisted successfully.
func (m *ServerManager) UpdateServerConfiguration(id string, update ServerConfigurationUpdate) (*Server, error) {
	serverLifecycleMutex.Lock()
	defer serverLifecycleMutex.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	stateMutex.Lock()
	defer stateMutex.Unlock()
	server, ok := m.servers[id]
	if !ok {
		return nil, ErrServerMissing
	}
	if server.starting || server.Stopping || server.Running &&
		(update.BindIP != nil || update.Port != nil || update.Autostart != nil || update.WatchdogInterval != nil) {
		return nil, ErrServerActive
	}
	candidate := *server
	if update.Name != nil {
		name := strings.TrimSpace(*update.Name)
		if len(name) > 128 || strings.IndexFunc(name, func(character rune) bool { return character < 32 || character == 127 }) >= 0 {
			return nil, fmt.Errorf("%w: name is too long or contains control characters", ErrInvalidServerConfiguration)
		}
		candidate.Name = name
	}
	if update.BindIP != nil {
		if net.ParseIP(*update.BindIP) == nil {
			return nil, fmt.Errorf("%w: bind_ip must be an IP address", ErrInvalidServerConfiguration)
		}
		candidate.BindIP = *update.BindIP
	}
	if update.Port != nil {
		if *update.Port < 1 || *update.Port > 65535 {
			return nil, fmt.Errorf("%w: port must be between 1 and 65535", ErrInvalidServerConfiguration)
		}
		for otherID, other := range m.servers {
			if otherID != id && other.Port == *update.Port {
				return nil, ErrServerPortConflict
			}
		}
		candidate.Port = *update.Port
	}
	if update.Autostart != nil {
		candidate.Autostart = *update.Autostart
	}
	if update.WatchdogInterval != nil {
		if *update.WatchdogInterval < 0 {
			return nil, fmt.Errorf("%w: watchdog_interval must be nonnegative", ErrInvalidServerConfiguration)
		}
		candidate.WatchdogInterval = *update.WatchdogInterval
	}
	if update.UseMimalloc != nil {
		candidate.UseMimalloc = *update.UseMimalloc
	}
	for i := range m.catalog.Servers {
		if m.catalog.Servers[i].ID != id {
			continue
		}
		previous := m.catalog.Servers[i]
		next := previous
		next.Name, next.BindIP, next.Port = candidate.Name, candidate.BindIP, candidate.Port
		next.Autostart, next.WatchdogInterval, next.UseMimalloc = candidate.Autostart, candidate.WatchdogInterval, candidate.UseMimalloc
		m.catalog.Servers[i] = next
		if err := m.saveCatalogLocked(); err != nil {
			m.catalog.Servers[i] = previous
			return nil, err
		}
		server.Name, server.BindIP, server.Port = next.Name, next.BindIP, next.Port
		server.Autostart, server.WatchdogInterval, server.UseMimalloc = next.Autostart, next.WatchdogInterval, next.UseMimalloc
		response := *server
		return &response, nil
	}
	return nil, ErrServerMissing
}
