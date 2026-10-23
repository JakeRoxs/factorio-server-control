package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
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
		resp = []factorio.Server{factorio.GetFactorioServer().ConfigurationSnapshot()}
		return
	}
	servers := manager.ListServers()
	snapshots := make([]factorio.Server, 0, len(servers))
	for _, server := range servers {
		snapshots = append(snapshots, server.ConfigurationSnapshot())
	}
	resp = snapshots
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

	var data *struct {
		Name      string `json:"name"`
		Version   string `json:"version"`
		BindIP    string `json:"bind_ip"`
		Port      int    `json:"port"`
		Autostart bool   `json:"autostart"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	decodeErr := decoder.Decode(&data)
	if decodeErr == nil && data == nil {
		decodeErr = errors.New("server request must be an object")
	}
	if decodeErr == nil {
		if trailing := decoder.Decode(new(interface{})); trailing != io.EOF {
			decodeErr = trailing
			if decodeErr == nil {
				decodeErr = errors.New("unexpected trailing server request data")
			}
		}
	}
	if decodeErr != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(decodeErr, &tooLarge) {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
		} else {
			w.WriteHeader(http.StatusBadRequest)
		}
		resp = "Invalid server request"
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
		if errors.Is(err, factorio.ErrInvalidServerConfiguration) {
			w.WriteHeader(http.StatusBadRequest)
			resp = err.Error()
		} else if errors.Is(err, factorio.ErrServerPortConflict) {
			w.WriteHeader(http.StatusConflict)
			resp = err.Error()
		} else {
			log.Printf("Managed server initialization failed: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			resp = "Server storage could not be initialized"
		}
		return
	}
	resp = server.ConfigurationSnapshot()
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
	resp = server.ConfigurationSnapshot()
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
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var data *factorio.ServerConfigurationUpdate
	err := decoder.Decode(&data)
	if err == nil && data == nil {
		err = errors.New("request must be a JSON object")
	}
	if err == nil {
		err = decoder.Decode(new(interface{}))
		if err == io.EOF {
			err = nil
		} else if err == nil {
			err = errors.New("multiple JSON values")
		}
	}
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
		} else {
			w.WriteHeader(http.StatusBadRequest)
		}
		resp = "Invalid server configuration request"
		return
	}
	server, err := manager.UpdateServerConfiguration(id, *data)
	if err != nil {
		switch {
		case errors.Is(err, factorio.ErrServerMissing):
			w.WriteHeader(http.StatusNotFound)
			resp = errServerNotFound
		case errors.Is(err, factorio.ErrServerActive):
			w.WriteHeader(http.StatusLocked)
			resp = "Only the name and next-start allocator preference can be changed while running. Stop the server before changing other configuration."
		case errors.Is(err, factorio.ErrInvalidServerConfiguration):
			w.WriteHeader(http.StatusBadRequest)
			resp = err.Error()
		case errors.Is(err, factorio.ErrServerPortConflict):
			w.WriteHeader(http.StatusConflict)
			resp = err.Error()
		default:
			log.Printf("Server configuration persistence failed: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			resp = "Server configuration could not be saved"
		}
		return
	}
	resp = server
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
		if errors.Is(err, factorio.ErrDefaultServer) {
			w.WriteHeader(http.StatusConflict)
		} else if errors.Is(err, factorio.ErrServerActive) {
			w.WriteHeader(http.StatusLocked)
		} else {
			log.Printf("Managed server deletion could not be persisted: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			resp = "Server deletion could not be saved"
			return
		}
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
		if id == factorio.DefaultServerID {
			return factorio.GetFactorioServer(), true
		}
		return nil, false
	}
	server, ok := manager.GetServer(id)
	return server, ok
}

func StartServerHandler(w http.ResponseWriter, r *http.Request) {
	StartServer(w, r)
}

func StopServerHandler(w http.ResponseWriter, r *http.Request) {
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

	if _, ok := manager.GetServer(id); !ok {
		w.WriteHeader(http.StatusNotFound)
		resp = errServerNotFound
		return
	}
	err := manager.Stop(id)
	if err != nil {
		if errors.Is(err, factorio.ErrServerActive) {
			w.WriteHeader(http.StatusConflict)
		} else {
			w.WriteHeader(http.StatusInternalServerError)
		}
		resp = err.Error()
		return
	}

	resp = true
}

func KillServerHandler(w http.ResponseWriter, r *http.Request) {
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

	if _, ok := manager.GetServer(id); !ok {
		w.WriteHeader(http.StatusNotFound)
		resp = errServerNotFound
		return
	}
	err := manager.Kill(id)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		resp = err.Error()
		return
	}

	resp = true
}

func SaveServerHandler(w http.ResponseWriter, r *http.Request) {
	var resp interface{}
	defer func() { WriteResponse(w, resp) }()
	setJSON(w)

	server, ok := serverFromRequest(r)
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		resp = errServerNotFound
		return
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

func CreateServerWorldHandler(w http.ResponseWriter, r *http.Request) {
	var resp interface{}
	defer func() { WriteResponse(w, resp) }()
	setJSON(w)

	id := mux.Vars(r)["serverID"]
	if id == "" {
		w.WriteHeader(http.StatusBadRequest)
		resp = "server id is required"
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxWorldGenerationRequestSize)
	var request factorio.WorldGenerationRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		resp = fmt.Sprintf("Unable to parse world settings: %s", err)
		return
	}
	if err := decoder.Decode(new(interface{})); err != io.EOF {
		w.WriteHeader(http.StatusBadRequest)
		resp = "Unable to parse world settings: request must contain one JSON object"
		return
	}
	if err := factorio.ValidateWorldGenerationRequest(request, false); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		resp = fmt.Sprintf("Invalid world settings: %s", err)
		return
	}
	save, err := factorio.CreateServerWorld(id, request)
	if err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, factorio.ErrWorldGenerationBusy), errors.Is(err, factorio.ErrSaveAlreadyExists):
			status = http.StatusConflict
		case errors.Is(err, factorio.ErrFactorioMustBeStopped):
			status = http.StatusLocked
		}
		w.WriteHeader(status)
		resp = fmt.Sprintf("Unable to create world for server %s: %s", id, err)
		return
	}
	resp = save
}
