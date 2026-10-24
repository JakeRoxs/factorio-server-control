package api

import (
	"github.com/gorilla/mux"
	"mime"
	"net/http"

	"github.com/OpenFactorioServerManager/factorio-server-manager/factorio"
)

// Server-scoped checkpoint endpoints. SelectedServerMiddleware resolves the
// target server from the path, header or query and fail-closes with a 404 when
// the server is not a managed server. Each handler resolves the server through
// serverFromRequest and binds the operation to that server's save directory,
// RCON credentials, and per-server checkpoint store. Each server owns its own
// checkpoint archives, settings, retention, and error state under
// checkpoints/servers/{serverID}.

func ListCheckpointsForServer(w http.ResponseWriter, r *http.Request) {
	server, found := serverFromRequest(r)
	if !found {
		http.Error(w, errServerNotFound, http.StatusNotFound)
		return
	}
	state, err := factorio.ListCheckpointsForServer(server)
	if err != nil {
		writeCheckpointError(w, "Unable to load checkpoints", err)
		return
	}
	writeCheckpointResponse(w, state, http.StatusOK)
}

func SaveCheckpointSettingsForServer(w http.ResponseWriter, r *http.Request) {
	server, found := serverFromRequest(r)
	if !found {
		http.Error(w, errServerNotFound, http.StatusNotFound)
		return
	}
	var settings factorio.CheckpointSettings
	if !decodeCheckpointRequest(w, r, &settings) {
		return
	}
	state, err := factorio.UpdateCheckpointSettingsForServer(server, settings)
	if err != nil {
		writeCheckpointError(w, "Unable to save checkpoint settings", err)
		return
	}
	writeCheckpointResponse(w, state, http.StatusOK)
}

func CreateCheckpointForServer(w http.ResponseWriter, r *http.Request) {
	server, found := serverFromRequest(r)
	if !found {
		http.Error(w, errServerNotFound, http.StatusNotFound)
		return
	}
	state, err := factorio.CreateCheckpointForServer(server, factorio.CheckpointTriggerManual)
	if err != nil {
		writeCheckpointError(w, "Unable to create checkpoint", err)
		return
	}
	writeCheckpointResponse(w, state, http.StatusCreated)
}

func RestoreCheckpointForServer(w http.ResponseWriter, r *http.Request) {
	server, found := serverFromRequest(r)
	if !found {
		http.Error(w, errServerNotFound, http.StatusNotFound)
		return
	}
	save, err := factorio.RestoreCheckpointForServer(server, mux.Vars(r)["checkpoint"])
	if err != nil {
		writeCheckpointError(w, "Unable to restore checkpoint", err)
		return
	}
	writeCheckpointResponse(w, save, http.StatusCreated)
}

func DeleteCheckpointForServer(w http.ResponseWriter, r *http.Request) {
	server, found := serverFromRequest(r)
	if !found {
		http.Error(w, errServerNotFound, http.StatusNotFound)
		return
	}
	state, err := factorio.DeleteCheckpointForServer(server, mux.Vars(r)["checkpoint"])
	if err != nil {
		writeCheckpointError(w, "Unable to delete checkpoint", err)
		return
	}
	writeCheckpointResponse(w, state, http.StatusOK)
}

func DownloadCheckpointForServer(w http.ResponseWriter, r *http.Request) {
	server, found := serverFromRequest(r)
	if !found {
		http.Error(w, errServerNotFound, http.StatusNotFound)
		return
	}
	checkpoint, path, err := factorio.FindCheckpointFileForServer(server, mux.Vars(r)["checkpoint"])
	if err != nil {
		writeCheckpointError(w, "Unable to download checkpoint", err)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": checkpoint.FileName}))
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFile(w, r, path)
}
