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
		if route := mux.CurrentRoute(r); route != nil && route.GetName() == "" {
			next.ServeHTTP(w, r)
			return
		}
		path := strings.TrimSuffix(r.URL.Path, "/")
		// Manager-wide discovery must remain usable even when a bookmarked
		// workspace refers to a server that has since been deleted.
		if managerBypassRoute(path, r.Method) {
			next.ServeHTTP(w, r)
			return
		}
		id := resolveServerID(r)
		if id == "" {
			next.ServeHTTP(w, r)
			return
		}
		r, ok := resolveAndBindServer(w, r, id)
		if !ok {
			return
		}
		if id != factorio.DefaultServerID && !secondaryServerRouteSupported(path, r.Method) {
			http.Error(w, "This operation is not available for additional servers yet", http.StatusConflict)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// managerBypassRoute reports routes that must remain usable through manager-wide
// discovery even when a bookmarked workspace refers to a server that has since
// been deleted.
func managerBypassRoute(path, method string) bool {
	if path == "/api/servers" || path == "/api/servers/next" || strings.HasPrefix(path, "/api/user/") || path == "/api/logout" {
		return true
	}
	if path == "/api/server/autostart" || path == "/api/mods/portal/preferences" || path == "/api/backups/limits" || path == "/api/mods/packs/list" {
		return true
	}
	if path == "/api/profiles" && method == http.MethodGet {
		return true
	}
	return false
}

// resolveServerID returns the server selection from route vars, then the
// X-FSM-Server-ID header, then the server query parameter.
func resolveServerID(r *http.Request) string {
	id := mux.Vars(r)["serverID"]
	if id != "" {
		return id
	}
	id = r.Header.Get("X-FSM-Server-ID")
	if id != "" {
		return id
	}
	return r.URL.Query().Get("server")
}

// resolveAndBindServer validates the selection, binds selectedServerID onto the
// request, and resolves the server. It reports validation/not-found errors
// through the response writer and returns false when the request must stop.
func resolveAndBindServer(w http.ResponseWriter, r *http.Request, id string) (*http.Request, bool) {
	if err := factorio.ValidatePathElement(id); err != nil {
		http.Error(w, "Invalid server selection", http.StatusBadRequest)
		return r, false
	}
	vars := mux.Vars(r)
	if vars == nil {
		vars = make(map[string]string)
	}
	vars["selectedServerID"] = id
	r = mux.SetURLVars(r, vars)
	if _, found := serverFromRequest(r); !found {
		http.Error(w, errServerNotFound, http.StatusNotFound)
		return r, false
	}
	return r, true
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
