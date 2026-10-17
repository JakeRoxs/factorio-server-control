package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/OpenFactorioServerManager/factorio-server-manager/bootstrap"
	"github.com/OpenFactorioServerManager/factorio-server-manager/factorio"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"
)

func TestLegacyCreateSaveNeverOverwritesExistingSave(t *testing.T) {
	file, err := os.CreateTemp(bootstrap.GetConfig().FactorioSavesDir, "legacy-existing-*.zip")
	require.NoError(t, err)
	path := file.Name()
	t.Cleanup(func() { _ = os.Remove(path) })
	_, err = file.WriteString("existing world must survive")
	require.NoError(t, err)
	require.NoError(t, file.Close())
	recorder := httptest.NewRecorder()
	request := mux.SetURLVars(httptest.NewRequest(http.MethodPost, "/api/saves/create/fixture.zip", nil), map[string]string{"save": filepath.Base(path)})
	CreateSaveHandler(recorder, request)
	require.Equal(t, http.StatusConflict, recorder.Code)
	contents, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "existing world must survive", string(contents))
}

func TestLegacyCreateSaveRejectsRunningServerEvenWithoutMiddleware(t *testing.T) {
	server := factorio.GetFactorioServer()
	original := server.GetRunning()
	server.SetRunning(true)
	t.Cleanup(func() { server.SetRunning(original) })
	_, err := factorio.CreateSave(filepath.Join(bootstrap.GetConfig().FactorioSavesDir, "legacy-new.zip"))
	require.ErrorIs(t, err, factorio.ErrFactorioMustBeStopped)
}
