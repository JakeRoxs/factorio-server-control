package api

import (
	"net/http"
	"strings"

	"github.com/OpenFactorioServerManager/factorio-server-manager/bootstrap"
	"github.com/OpenFactorioServerManager/factorio-server-manager/factorio"
	"github.com/gorilla/mux"
)

func requestSavesDirectory(r *http.Request) string {
	if server, found := serverFromRequest(r); found && server.SavesDir != "" {
		return server.SavesDir
	}
	return bootstrap.GetConfig().FactorioSavesDir
}

// Selection is request-local. Never change the process-wide default runtime to
// service a different browser's workspace.
func SelectedServerMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if route := mux.CurrentRoute(r); route != nil {
			if route.GetName() == "" {
				next.ServeHTTP(w, r)
				return
			}
		}
		path := strings.TrimSuffix(r.URL.Path, "/")
		// Manager-wide discovery must remain usable even when a bookmarked
		// workspace refers to a server that has since been deleted.
		if path == "/api/servers" || path == "/api/servers/next" || strings.HasPrefix(path, "/api/user/") || path == "/api/logout" ||
			path == "/api/profiles" && r.Method == http.MethodGet || path == "/api/server/autostart" ||
			path == "/api/mods/portal/preferences" || path == "/api/backups/limits" || path == "/api/mods/packs/list" {
			next.ServeHTTP(w, r)
			return
		}
		vars := mux.Vars(r)
		id := vars["serverID"]
		if id == "" {
			id = r.Header.Get("X-FSM-Server-ID")
			if id == "" {
				id = r.URL.Query().Get("server")
			}
		}
		if id == "" {
			next.ServeHTTP(w, r)
			return
		}
		if factorio.ValidatePathElement(id) != nil {
			http.Error(w, "Invalid server selection", http.StatusBadRequest)
			return
		}
		if vars == nil {
			vars = make(map[string]string)
		}
		vars["selectedServerID"] = id
		r = mux.SetURLVars(r, vars)
		if _, found := serverFromRequest(r); !found {
			http.Error(w, errServerNotFound, http.StatusNotFound)
			return
		}
		if id != factorio.DefaultServerID && !secondaryServerRouteSupported(path, r.Method) {
			http.Error(w, "This operation is not available for additional servers yet", http.StatusConflict)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func secondaryServerRouteSupported(path, method string) bool {
	if strings.HasPrefix(path, "/api/servers") || strings.HasPrefix(path, "/api/user/") || path == "/api/logout" {
		return true
	}
	switch path {
	case "/api/server/start", "/api/server/stop", "/api/server/kill", "/api/server/status", "/api/server/facVersion",
		"/api/server/autostart",
		"/api/settings", "/api/settings/update", "/api/config", "/api/log/tail", "/api/saves/list", "/api/saves/upload",
		"/api/mods/list", "/api/mods/toggle", "/api/mods/delete", "/api/mods/update", "/api/mods/upload", "/api/mods/download":
		return true
	case "/api/profiles", "/api/mods/packs/list", "/api/backups/limits":
		return method == http.MethodGet
	}
	return strings.HasPrefix(path, "/api/saves/dl/") || strings.HasPrefix(path, "/api/saves/rm/") ||
		strings.HasPrefix(path, "/api/mods/portal/") && (method == http.MethodGet || strings.HasSuffix(path, "/preferences"))
}

func ServerCommandHandler(w http.ResponseWriter, r *http.Request) {
	server, found := serverFromRequest(r)
	if !found {
		http.Error(w, errServerNotFound, http.StatusNotFound)
		return
	}
	var request struct {
		Command string `json:"command"`
	}
	if response, err := ReadFromRequestBody(w, r, &request); err != nil {
		WriteResponse(w, response)
		return
	}
	if strings.TrimSpace(request.Command) == "" || len(request.Command) > 4096 || strings.ContainsAny(request.Command, "\r\n\x00") {
		http.Error(w, "Enter one console command of at most 4096 bytes", http.StatusBadRequest)
		return
	}
	if !server.GetRunning() || server.IsStopping() {
		http.Error(w, "Server is not ready for console commands", http.StatusConflict)
		return
	}
	response, err := server.RunPersistentRCONCommand(request.Command)
	if err != nil {
		http.Error(w, "Console command could not be completed", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	WriteResponse(w, response)
}
