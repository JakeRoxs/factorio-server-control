package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"

	"github.com/OpenFactorioServerManager/factorio-server-manager/factorio"
)

func findRoute(name string) (Route, bool) {
	for _, route := range apiRoutes {
		if route.Name == name {
			return route, true
		}
	}
	return Route{}, false
}

func TestServerScopedCheckpointRoutesAreRegisteredAndScoped(t *testing.T) {
	for _, name := range []string{
		"ListCheckpointsForServer",
		"SaveCheckpointSettingsForServer",
		"CreateCheckpointForServer",
		"DownloadCheckpointForServer",
		"RestoreCheckpointForServer",
		"DeleteCheckpointForServer",
	} {
		_, ok := findRoute(name)
		require.True(t, ok, name+" route must be registered")
	}

	restore, ok := findRoute("RestoreCheckpointForServer")
	require.True(t, ok, "RestoreCheckpointForServer route must be registered")
	require.True(t, restore.ServerOff, "restore must remain server-off")

	// The restore route must bind to the per-server checkpoint path.
	for _, route := range apiRoutes {
		if route.Name == "RestoreCheckpointForServer" {
			require.Contains(t, route.Pattern, "/servers/{serverID}/checkpoints/{checkpoint}/restore")
		}
	}
}

func TestServerScopedCheckpointRoutesResolveSelectedServer(t *testing.T) {
	manager, err := factorio.InitServerManager()
	require.NoError(t, err)
	serverA, err := manager.CreateServer("A", "stable", "127.0.0.1", 34200, false)
	require.NoError(t, err)
	serverB, err := manager.CreateServer("B", "stable", "127.0.0.1", 34201, false)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, manager.DeleteServer(serverB.ID)); require.NoError(t, manager.DeleteServer(serverA.ID)) })

	router := mux.NewRouter()
	router.Use(SelectedServerMiddleware)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	router.Handle("/api/servers/{serverID}/checkpoints", handler).Methods(http.MethodGet).Name("Checkpoints")
	router.Handle("/api/servers/{serverID}/checkpoints/settings", handler).Methods(http.MethodPut).Name("CheckpointSettings")

	// A selected non-default server must reach the scoped handler.
	for _, id := range []string{serverA.ID, serverB.ID} {
		request := httptest.NewRequest(http.MethodGet, "/api/servers/"+id+"/checkpoints", nil)
		request.Header.Set("X-FSM-Server-ID", id)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, request)
		require.Equal(t, http.StatusOK, rec.Code, "server %s checkpoint route must resolve the selected server", id)
	}

	// A missing server must fail closed.
	request := httptest.NewRequest(http.MethodGet, "/api/servers/missing/checkpoints", nil)
	request.Header.Set("X-FSM-Server-ID", "missing")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, request)
	require.Equal(t, http.StatusNotFound, rec.Code, "missing server must fail closed")
}
