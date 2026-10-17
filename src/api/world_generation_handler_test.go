package api

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/OpenFactorioServerManager/factorio-server-manager/factorio"
	"github.com/stretchr/testify/require"
)

func TestWorldGenerationRequestDecode(t *testing.T) {
	for _, body := range []string{"", "[]", `{"unknown":true}`, `{"seed":1} {}`, `{"seed":-1}`, `{"name":"` + strings.Repeat("x", maxWorldGenerationRequestSize) + `"}`} {
		recorder := httptest.NewRecorder()
		_, ok := decodeWorldGenerationRequest(recorder, httptest.NewRequest(http.MethodPost, "/api/saves/generation/preview", strings.NewReader(body)))
		require.False(t, ok)
		require.Equal(t, http.StatusBadRequest, recorder.Code)
	}
	recorder := httptest.NewRecorder()
	request, ok := decodeWorldGenerationRequest(recorder, httptest.NewRequest(http.MethodPost, "/api/saves/generation/preview", strings.NewReader(`{"seed":42,"planet":"nauvis","preset":"default"}`)))
	require.True(t, ok)
	require.Equal(t, uint64(42), request.Seed)
	require.Equal(t, "nauvis", request.Planet)
}

func TestWorldGenerationErrorStatuses(t *testing.T) {
	for _, test := range []struct {
		err    error
		status int
	}{
		{factorio.ErrWorldGenerationBusy, http.StatusConflict},
		{factorio.ErrSaveAlreadyExists, http.StatusConflict},
		{factorio.ErrFactorioMustBeStopped, http.StatusLocked},
		{errors.New("engine failed"), http.StatusInternalServerError},
	} {
		recorder := httptest.NewRecorder()
		writeWorldGenerationError(recorder, "Unable to create world", fmt.Errorf("wrapped: %w", test.err))
		require.Equal(t, test.status, recorder.Code)
	}
}
