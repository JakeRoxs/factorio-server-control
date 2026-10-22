package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/OpenFactorioServerManager/factorio-server-manager/factorio"
	"github.com/gorilla/mux"
)

func TestServerConfigurationRouteAllowsHandlerToValidateLivePreferences(t *testing.T) {
	for _, route := range apiRoutes {
		if route.Name == "UpdateServer" {
			if !routeRequiresAdministrator(route) {
				t.Fatal("server configuration changes must remain administrator-only")
			}
			if route.ServerOff {
				t.Fatal("route rejects next-start allocator preferences before the handler can validate them")
			}
			return
		}
	}
	t.Fatal("server configuration route is missing")
}

func TestManagedCreationDoesNotRequireStoppingTheDefaultGame(t *testing.T) {
	for _, route := range apiRoutes {
		if route.Name == "CreateServer" {
			if route.ServerOff {
				t.Fatal("creating independent storage must not require stopping Server 1")
			}
			if !routeRequiresAdministrator(route) {
				t.Fatal("new runtime creation must remain administrator-only")
			}
			return
		}
	}
	t.Fatal("managed creation route is missing")
}

func TestManagedSaveRouteAllowsRunningGame(t *testing.T) {
	for _, route := range apiRoutes {
		if route.Name == "SaveServer" {
			if route.ServerOff || !routeRequiresAdministrator(route) {
				t.Fatal("RCON save must be administrator-only without a stopped-server guard")
			}
			return
		}
	}
	t.Fatal("managed RCON save route is missing")
}

func TestManagedCreationRejectsMalformedAndOversizedRequests(t *testing.T) {
	for _, request := range []struct {
		body string
		code int
	}{
		{`null`, http.StatusBadRequest},
		{`{} {}`, http.StatusBadRequest},
		{`{"config_dir":"private"}`, http.StatusBadRequest},
		{`{"name":"` + strings.Repeat("a", 9000) + `"}`, http.StatusRequestEntityTooLarge},
		{`{}` + strings.Repeat(" ", 9000), http.StatusRequestEntityTooLarge},
	} {
		response := httptest.NewRecorder()
		CreateServerHandler(response, httptest.NewRequest(http.MethodPost, "/api/servers", strings.NewReader(request.body)))
		if response.Code != request.code {
			t.Fatalf("malformed creation request returned %d, want %d", response.Code, request.code)
		}
	}
}

func TestServerConfigurationHTTPRoundTripWhileRunning(t *testing.T) {
	manager, err := factorio.InitServerManager()
	if err != nil {
		t.Fatal(err)
	}
	server, ok := manager.GetServer(factorio.DefaultServerID)
	if !ok {
		t.Fatal("default runtime was not initialized")
	}
	previous := server.ConfigurationSnapshot()
	server.SetRunning(true)
	t.Cleanup(func() {
		server.SetRunning(false)
		_, err := manager.UpdateServerConfiguration(server.ID, factorio.ServerConfigurationUpdate{Name: &previous.Name, UseMimalloc: &previous.UseMimalloc})
		if err != nil {
			t.Error(err)
		}
		server.SetRunning(previous.Running)
	})
	router := mux.NewRouter()
	for _, route := range apiRoutes {
		if route.Name != "UpdateServer" && route.Name != "GetServer" {
			continue
		}
		var handler http.Handler = route.HandlerFunc
		if route.ServerOff {
			handler = ServerOffMiddleware(handler)
		}
		router.Handle("/api"+route.Pattern, handler).Methods(route.Method)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPut, "/api/servers/1", strings.NewReader(`{"use_mimalloc":true}`)))
	if response.Code != http.StatusOK {
		t.Fatalf("running allocator save returned %d: %s", response.Code, response.Body.String())
	}
	for _, invalid := range []struct {
		body string
		code int
	}{
		{`{"use_mimalloc":false,"port":34200}`, http.StatusLocked},
		{`{"use_mimalloc":true} {}`, http.StatusBadRequest},
		{`{"unexpected":true}`, http.StatusBadRequest},
		{`null`, http.StatusBadRequest},
		{`{"name":"` + strings.Repeat("a", 9000) + `"}`, http.StatusRequestEntityTooLarge},
	} {
		response = httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodPut, "/api/servers/1", strings.NewReader(invalid.body)))
		if response.Code != invalid.code {
			t.Fatalf("invalid configuration returned %d, want %d", response.Code, invalid.code)
		}
	}
	response = httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/servers/1", nil))
	var saved factorio.Server
	if err := json.Unmarshal(response.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	if !saved.UseMimalloc || !saved.Running {
		t.Fatal("saved allocator preference or running state was lost on readback")
	}
	if strings.Contains(response.Body.String(), "rcon_pass") {
		t.Fatal("configuration response exposed RCON credentials")
	}
}

func TestServerActionsRejectUnknownTarget(t *testing.T) {
	for action, handler := range map[string]http.HandlerFunc{"start": StartServerHandler, "save": SaveServerHandler} {
		request := mux.SetURLVars(httptest.NewRequest(http.MethodPost, "/api/servers/missing/"+action,
			strings.NewReader(`{"bindip":"127.0.0.1","port":34198,"savefile":"world.zip"}`)), map[string]string{"serverID": "missing"})
		response := httptest.NewRecorder()
		handler(response, request)
		if response.Code != http.StatusNotFound {
			t.Fatalf("unknown %s target returned %d, want 404", action, response.Code)
		}
	}
}

func TestServerProfileRoutesOwnProfileDataLock(t *testing.T) {
	for _, path := range []string{
		"/api/servers",
		"/api/server/release/install",
		"/api/servers/2/profiles",
		"/api/servers/2/profiles/0123456789abcdef/assign",
		"/api/servers/2/profiles/0123456789abcdef/apply",
		"/api/servers/2/profiles/",
	} {
		done := make(chan struct{})
		handler := ProfileDataMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			// This operation acquires the same exclusive gate as profile writes,
			// then rejects its invalid input without changing persistent data.
			_, _ = factorio.PreviewModPackMerge("../invalid")
			close(done)
		}))
		request := mux.SetURLVars(httptest.NewRequest(http.MethodPost, path, nil), map[string]string{"serverID": "2"})
		go handler.ServeHTTP(httptest.NewRecorder(), request)
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatalf("%s recursively acquired the profile-data gate", path)
		}
	}
}
