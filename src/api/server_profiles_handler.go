package api

import (
	"errors"
	"fmt"
	"log"
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
	if !decodeProfileRequest(w, r, &data) {
		return
	}

	state, err := factorio.CreateServerProfile(id, data.Name, data.Description)
	if err != nil {
		switch {
		case errors.Is(err, factorio.ErrProfileNameConflict):
			w.WriteHeader(http.StatusConflict)
		case errors.Is(err, factorio.ErrServerMissing):
			w.WriteHeader(http.StatusNotFound)
		case errors.Is(err, factorio.ErrProfileServerActive):
			w.WriteHeader(http.StatusLocked)
		case errors.Is(err, factorio.ErrInvalidProfile):
			w.WriteHeader(http.StatusBadRequest)
		default:
			w.WriteHeader(http.StatusInternalServerError)
			log.Printf("Server preset capture failed: %v", err)
			resp = "The preset could not be captured"
			return
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
	if !decodeProfileRequest(w, r, &data) {
		return
	}

	state, err := factorio.CreateGlobalProfile(data.Name, data.Description)
	if err != nil {
		switch {
		case errors.Is(err, factorio.ErrProfileNameConflict):
			w.WriteHeader(http.StatusConflict)
		case errors.Is(err, factorio.ErrServerMissing):
			w.WriteHeader(http.StatusNotFound)
		case errors.Is(err, factorio.ErrProfileServerActive):
			w.WriteHeader(http.StatusLocked)
		case errors.Is(err, factorio.ErrInvalidProfile):
			w.WriteHeader(http.StatusBadRequest)
		default:
			w.WriteHeader(http.StatusInternalServerError)
			log.Printf("Default-server preset capture failed: %v", err)
			resp = "The preset could not be captured"
			return
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
		switch {
		case errors.Is(err, factorio.ErrProfileNotFound), errors.Is(err, factorio.ErrServerMissing):
			w.WriteHeader(http.StatusNotFound)
		case errors.Is(err, factorio.ErrProfileServerActive):
			w.WriteHeader(http.StatusLocked)
		case errors.Is(err, factorio.ErrInvalidProfile):
			w.WriteHeader(http.StatusBadRequest)
		default:
			w.WriteHeader(http.StatusInternalServerError)
			log.Printf("Global profile application failed: %v", err)
			resp = "Unable to apply the selected profile"
			return
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
		switch {
		case errors.Is(err, factorio.ErrProfileNotFound), errors.Is(err, factorio.ErrServerMissing):
			w.WriteHeader(http.StatusNotFound)
		case errors.Is(err, factorio.ErrProfileServerActive):
			w.WriteHeader(http.StatusLocked)
		case errors.Is(err, factorio.ErrInvalidProfile):
			w.WriteHeader(http.StatusBadRequest)
		default:
			w.WriteHeader(http.StatusInternalServerError)
			log.Printf("Server profile assignment failed: %v", err)
			resp = "Unable to assign the selected profile"
			return
		}
		resp = fmt.Sprintf("error assigning profile: %s", err)
		return
	}
	resp = true
}
