package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/OpenFactorioServerManager/factorio-server-manager/factorio"
	"github.com/gorilla/mux"
)

func ListServerProfilesHandler(w http.ResponseWriter, r *http.Request) {
	var resp interface{}
	defer func() { WriteResponse(w, resp) }()
	setJSON(w)

	id := mux.Vars(r)["serverID"]
	if id == "" {
		id = factorio.DefaultServerID
	}

	state, err := factorio.ListProfilesForServer(id)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		resp = fmt.Sprintf("error listing profiles: %s", err)
		return
	}
	resp = state
}

func CreateServerProfileHandler(w http.ResponseWriter, r *http.Request) {
	var resp interface{}
	defer func() { WriteResponse(w, resp) }()
	setJSON(w)

	id := mux.Vars(r)["serverID"]
	if id == "" {
		id = factorio.DefaultServerID
	}

	var data struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		resp = fmt.Sprintf("invalid profile request: %s", err)
		return
	}

	state, err := factorio.CreateServerProfile(id, data.Name, data.Description)
	if err != nil {
		switch err {
		case factorio.ErrProfileNameConflict:
			w.WriteHeader(http.StatusConflict)
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
		resp = fmt.Sprintf("error creating profile: %s", err)
		return
	}
	resp = state
}

func CreateGlobalProfileHandler(w http.ResponseWriter, r *http.Request) {
	var resp interface{}
	defer func() { WriteResponse(w, resp) }()
	setJSON(w)

	var data struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		resp = fmt.Sprintf("invalid profile request: %s", err)
		return
	}

	state, err := factorio.CreateGlobalProfile(data.Name, data.Description)
	if err != nil {
		switch err {
		case factorio.ErrProfileNameConflict:
			w.WriteHeader(http.StatusConflict)
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
		resp = fmt.Sprintf("error creating global profile: %s", err)
		return
	}
	resp = state
}

func ApplyGlobalProfileHandler(w http.ResponseWriter, r *http.Request) {
	var resp interface{}
	defer func() { WriteResponse(w, resp) }()
	setJSON(w)

	serverID := mux.Vars(r)["serverID"]
	profileID := mux.Vars(r)["profile"]
	if serverID == "" {
		serverID = factorio.DefaultServerID
	}

	state, err := factorio.ApplyGlobalProfileToServer(serverID, profileID)
	if err != nil {
		switch err {
		case factorio.ErrProfileNotFound:
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
		resp = fmt.Sprintf("error applying global profile: %s", err)
		return
	}
	resp = state
}

func AssignServerProfileHandler(w http.ResponseWriter, r *http.Request) {
	var resp interface{}
	defer func() { WriteResponse(w, resp) }()
	setJSON(w)

	serverID := mux.Vars(r)["serverID"]
	profileID := mux.Vars(r)["profile"]
	if serverID == "" {
		serverID = factorio.DefaultServerID
	}

	err := factorio.AssignProfileToServer(serverID, profileID)
	if err != nil {
		switch err {
		case factorio.ErrProfileNotFound:
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
		resp = fmt.Sprintf("error assigning profile: %s", err)
		return
	}
	resp = true
}
