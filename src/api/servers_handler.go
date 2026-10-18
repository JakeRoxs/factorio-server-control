package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/OpenFactorioServerManager/factorio-server-manager/factorio"
	"github.com/gorilla/mux"
)

const (
	headerContentType = "Content-Type"
	contentTypeJSON   = "application/json;charset=UTF-8"
	errManagerNotInit = "server manager is not initialized"
	errServerNotFound = "server not found"
)

func setJSON(w http.ResponseWriter) {
	w.Header().Set(headerContentType, contentTypeJSON)
}

func ListServersHandler(w http.ResponseWriter, r *http.Request) {
	var resp interface{}
	defer func() { WriteResponse(w, resp) }()
	setJSON(w)

	manager := factorio.GetServerManager()
	if manager == nil {
		resp = []*factorio.Server{factorio.GetFactorioServer()}
		return
	}
	resp = manager.ListServers()
}

func PreviewNextServerHandler(w http.ResponseWriter, r *http.Request) {
	var resp interface{}
	defer func() { WriteResponse(w, resp) }()
	setJSON(w)

	manager := factorio.GetServerManager()
	if manager == nil {
		w.WriteHeader(http.StatusInternalServerError)
		resp = errManagerNotInit
		return
	}
	id, name, port := manager.PreviewNextServer()
	resp = map[string]interface{}{
		"id":   id,
		"name": name,
		"port": port,
	}
}

func CreateServerHandler(w http.ResponseWriter, r *http.Request) {
	var resp interface{}
	defer func() { WriteResponse(w, resp) }()
	setJSON(w)

	var data struct {
		Name      string `json:"name"`
		Version   string `json:"version"`
		BindIP    string `json:"bind_ip"`
		Port      int    `json:"port"`
		Autostart bool   `json:"autostart"`
	}
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		resp = fmt.Sprintf("invalid server request: %s", err)
		return
	}

	manager := factorio.GetServerManager()
	if manager == nil {
		w.WriteHeader(http.StatusInternalServerError)
		resp = errManagerNotInit
		return
	}
	server, err := manager.CreateServer(data.Name, data.Version, data.BindIP, data.Port, data.Autostart)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		resp = fmt.Sprintf("error creating server: %s", err)
		return
	}
	resp = server
}

func GetServerHandler(w http.ResponseWriter, r *http.Request) {
	var resp interface{}
	defer func() { WriteResponse(w, resp) }()
	setJSON(w)

	id := mux.Vars(r)["serverID"]
	if id == "" {
		id = factorio.DefaultServerID
	}

	manager := factorio.GetServerManager()
	if manager == nil {
		w.WriteHeader(http.StatusInternalServerError)
		resp = errManagerNotInit
		return
	}
	server, ok := manager.GetServer(id)
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		resp = errServerNotFound
		return
	}
	resp = server
}

type serverUpdateRequest struct {
	Name             *string `json:"name"`
	BindIP           *string `json:"bind_ip"`
	Port             *int    `json:"port"`
	Autostart        *bool   `json:"autostart"`
	WatchdogInterval *int    `json:"watchdog_interval"`
	UseMimalloc      *bool   `json:"use_mimalloc"`
}

func UpdateServerHandler(w http.ResponseWriter, r *http.Request) {
	var resp interface{}
	defer func() { WriteResponse(w, resp) }()
	setJSON(w)

	id := mux.Vars(r)["serverID"]
	if id == "" {
		id = factorio.DefaultServerID
	}

	manager := factorio.GetServerManager()
	if manager == nil {
		w.WriteHeader(http.StatusInternalServerError)
		resp = errManagerNotInit
		return
	}
	server, ok := manager.GetServer(id)
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		resp = errServerNotFound
		return
	}

	var data serverUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		resp = fmt.Sprintf("invalid server request: %s", err)
		return
	}

	if data.Port != nil {
		if err := validatePortRange(*data.Port); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			resp = err.Error()
			return
		}
		if conflictID, found := findPortConflict(*data.Port, manager, server.ID); found {
			w.WriteHeader(http.StatusConflict)
			resp = fmt.Sprintf("port %d is already used by server %s", *data.Port, conflictID)
			return
		}
	}
	if data.WatchdogInterval != nil && *data.WatchdogInterval < 0 {
		w.WriteHeader(http.StatusBadRequest)
		resp = "watchdog_interval must be >= 0"
		return
	}

	applyServerUpdates(server, data)

	if err := manager.UpdateServer(server); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		resp = err.Error()
		return
	}
	resp = server
}

func validatePortRange(port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535")
	}
	return nil
}

func findPortConflict(port int, manager *factorio.ServerManager, serverID string) (string, bool) {
	for _, other := range manager.ListServers() {
		if other.ID != serverID && other.Port == port {
			return other.ID, true
		}
	}
	return "", false
}

func applyServerUpdates(server *factorio.Server, data serverUpdateRequest) {
	if data.Name != nil {
		server.Name = *data.Name
	}
	if data.BindIP != nil {
		if server.BindIP != *data.BindIP {
			server.PendingRestart = server.Running
		}
		server.BindIP = *data.BindIP
	}
	if data.Port != nil {
		if server.Port != *data.Port {
			server.PendingRestart = server.Running
		}
		server.Port = *data.Port
	}
	if data.Autostart != nil {
		server.Autostart = *data.Autostart
	}
	if data.WatchdogInterval != nil {
		server.WatchdogInterval = *data.WatchdogInterval
	}
	if data.UseMimalloc != nil {
		server.UseMimalloc = *data.UseMimalloc
	}
}

func DeleteServerHandler(w http.ResponseWriter, r *http.Request) {
	var resp interface{}
	defer func() { WriteResponse(w, resp) }()
	setJSON(w)

	id := mux.Vars(r)["serverID"]
	if id == "" {
		w.WriteHeader(http.StatusBadRequest)
		resp = "server id is required"
		return
	}

	manager := factorio.GetServerManager()
	if manager == nil {
		w.WriteHeader(http.StatusInternalServerError)
		resp = errManagerNotInit
		return
	}
	server, ok := manager.GetServer(id)
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		resp = errServerNotFound
		return
	}
	if server.GetRunning() {
		w.WriteHeader(http.StatusLocked)
		resp = "server is running"
		return
	}

	if err := manager.DeleteServer(id); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		resp = err.Error()
		return
	}
	resp = true
}

func serverFromRequest(r *http.Request) (*factorio.Server, bool) {
	id := mux.Vars(r)["serverID"]
	if id == "" {
		id = factorio.DefaultServerID
	}
	manager := factorio.GetServerManager()
	if manager == nil {
		return factorio.GetFactorioServer(), true
	}
	server, ok := manager.GetServer(id)
	return server, ok
}

func SaveServerHandler(w http.ResponseWriter, r *http.Request) {
	var resp interface{}
	defer func() { WriteResponse(w, resp) }()
	setJSON(w)

	id := mux.Vars(r)["serverID"]
	if id == "" {
		id = factorio.DefaultServerID
	}

	manager := factorio.GetServerManager()
	var server *factorio.Server
	if manager == nil {
		server = factorio.GetFactorioServer()
	} else {
		s, ok := manager.GetServer(id)
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			resp = errServerNotFound
			return
		}
		server = s
	}

	if !server.GetRunning() {
		w.WriteHeader(http.StatusConflict)
		resp = "server is not running"
		return
	}

	result, err := server.RunPersistentRCONCommand("/server-save")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		resp = fmt.Sprintf("error saving server: %s", err)
		return
	}
	resp = map[string]interface{}{
		"saved":  true,
		"result": result,
	}
}
