package api

import (
	"archive/zip"
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/OpenFactorioServerManager/factorio-server-manager/bootstrap"
	"github.com/OpenFactorioServerManager/factorio-server-manager/factorio"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"
)

func TestSelectedServerRequestsNeverMutateTheDefaultRuntime(t *testing.T) {
	manager, err := factorio.InitServerManager()
	require.NoError(t, err)
	server, err := manager.CreateServer("Scope test", "stable", "0.0.0.0", 0, false)
	require.NoError(t, err)
	primary, found := manager.GetServer("1")
	require.True(t, found)
	before := primary.SettingsSnapshot()
	primary.SetRunning(true)
	t.Cleanup(func() { primary.SetRunning(false); require.NoError(t, manager.DeleteServer(server.ID)) })
	run := func(method, path, body string, handler http.HandlerFunc) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		request.Header.Set("X-FSM-Server-ID", server.ID)
		response := httptest.NewRecorder()
		SelectedServerMiddleware(ServerOffMiddleware(handler)).ServeHTTP(response, request)
		return response
	}
	response := run(http.MethodPost, "/api/settings/update", `{"name":"Second world","max_players":11}`, UpdateServerSettings)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Equal(t, "Second world", server.SettingsSnapshot()["name"])
	require.Equal(t, before, primary.SettingsSnapshot())

	var contents bytes.Buffer
	archive := zip.NewWriter(&contents)
	entry, err := archive.Create("scope-world/level.dat")
	require.NoError(t, err)
	_, err = entry.Write([]byte("synthetic world bytes"))
	require.NoError(t, err)
	require.NoError(t, archive.Close())
	var body bytes.Buffer
	upload := multipart.NewWriter(&body)
	part, err := upload.CreateFormFile("savefile", "scope-world.zip")
	require.NoError(t, err)
	_, err = part.Write(contents.Bytes())
	require.NoError(t, err)
	require.NoError(t, upload.Close())
	request := httptest.NewRequest(http.MethodPost, "/api/saves/upload", &body)
	request.Header.Set("Content-Type", upload.FormDataContentType())
	request.Header.Set("X-FSM-Server-ID", server.ID)
	response = httptest.NewRecorder()
	SelectedServerMiddleware(ServerOffMiddleware(http.HandlerFunc(UploadSave))).ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	_, err = os.Stat(filepath.Join(server.SavesDir, "scope-world.zip"))
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(primary.SavesDir, "scope-world.zip"))
	require.True(t, os.IsNotExist(err))
	response = run(http.MethodGet, "/api/saves/list", "", ListServerSavesHandler)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), "scope-world.zip")
	response = run(http.MethodGet, "/api/mods/list", "", ListServerModsHandler)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	response = run(http.MethodGet, "/api/map-snapshot", "", func(w http.ResponseWriter, _ *http.Request) { t.Fatal("unsupported route reached default handler") })
	require.Equal(t, http.StatusConflict, response.Code)

	name := "scope-world.zip"
	updated, err := manager.UpdateServerConfiguration(server.ID, factorio.ServerConfigurationUpdate{SelectedSave: &name})
	require.NoError(t, err)
	require.Equal(t, name, updated.Savefile)
	response = run(http.MethodDelete, "/api/saves/rm/scope-world.zip", "", func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		vars["save"] = name
		RemoveSave(w, mux.SetURLVars(r, vars))
	})
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
}

func TestExplicitServerSelectionFailsClosed(t *testing.T) {
	for _, id := range []string{"missing", "../1", "CON"} {
		request := httptest.NewRequest(http.MethodPost, "/api/settings/update", nil)
		request.Header.Set("X-FSM-Server-ID", id)
		response := httptest.NewRecorder()
		SelectedServerMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("invalid selection reached handler") })).ServeHTTP(response, request)
		require.Contains(t, []int{http.StatusNotFound, http.StatusBadRequest}, response.Code)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/servers", nil)
	request.Header.Set("X-FSM-Server-ID", "deleted-server")
	response := httptest.NewRecorder()
	SelectedServerMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })).ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code, "manager discovery must remain available for recovery")
	router := mux.NewRouter()
	router.Use(SelectedServerMiddleware)
	router.HandleFunc("/api/map-snapshot", func(http.ResponseWriter, *http.Request) {}).Methods(http.MethodGet).Name("Snapshot")
	router.HandleFunc("/api/map-snapshot", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusMethodNotAllowed) })
	request = httptest.NewRequest(http.MethodDelete, "/api/map-snapshot", nil)
	request.Header.Set("X-FSM-Server-ID", "missing")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusMethodNotAllowed, response.Code)
	for _, route := range apiRoutes {
		if route.Name == "ServerConsoleCommand" {
			require.True(t, routeRequiresAdministrator(route))
			require.False(t, route.ServerOff)
		}
	}
}

func TestScopedLogPathMatchesTheRuntimeWriter(t *testing.T) {
	primary := factorio.Server{ID: "1", ConfigDir: filepath.Join(t.TempDir(), "config")}
	require.Equal(t, bootstrap.GetConfig().ConsoleLogFile, primary.ConsoleLogPath())
	secondary := factorio.Server{ID: "2", ConfigDir: filepath.Join(t.TempDir(), "config")}
	require.Equal(t, filepath.Join(filepath.Dir(secondary.ConfigDir), "console.log"), secondary.ConsoleLogPath())
}
