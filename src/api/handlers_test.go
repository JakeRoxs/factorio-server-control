package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/OpenFactorioServerManager/factorio-server-manager/factorio"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateStartRequest(t *testing.T) {
	// Empty savefile
	err := validateStartRequest(startServerRequest{BindIP: "127.0.0.1", Port: 8080})
	assert.Error(t, err)
	assert.Equal(t, "Error starting Factorio server: No save file provided", err.Error())

	// Invalid bind IP
	err = validateStartRequest(startServerRequest{Savefile: "save.zip", BindIP: "not-an-ip", Port: 8080})
	assert.Error(t, err)
	assert.Equal(t, "Error starting Factorio server: invalid IPv4 bind address", err.Error())

	// Port below 1
	err = validateStartRequest(startServerRequest{Savefile: "save.zip", BindIP: "127.0.0.1", Port: 0})
	assert.Error(t, err)
	assert.Equal(t, "Error starting Factorio server: port must be between 1 and 65535", err.Error())

	// Port above 65535
	err = validateStartRequest(startServerRequest{Savefile: "save.zip", BindIP: "127.0.0.1", Port: 65536})
	assert.Error(t, err)
	assert.Equal(t, "Error starting Factorio server: port must be between 1 and 65535", err.Error())

	// Valid request
	err = validateStartRequest(startServerRequest{Savefile: "save.zip", BindIP: "127.0.0.1", Port: 8080})
	assert.NoError(t, err)
}

func TestWriteMultipartParseError(t *testing.T) {
	// MaxBytesError -> 413 with upload limit message
	resp, status := writeMultipartParseError(100, &http.MaxBytesError{})
	assert.Equal(t, http.StatusRequestEntityTooLarge, status)
	assert.Contains(t, resp.(string), "exceeds the configured upload limit")

	// Other error -> 400 with parse error message
	resp, status = writeMultipartParseError(100, errors.New("some parse error"))
	assert.Equal(t, http.StatusBadRequest, status)
	assert.Equal(t, "Unable to parse save upload: some parse error", resp)
}

func TestStageUploadedSaveFileValidation(t *testing.T) {
	savesDir := t.TempDir()

	newFileHeader := func(name string, size int64) *multipart.FileHeader {
		return &multipart.FileHeader{
			Filename: name,
			Size:     size,
			Header:   map[string][]string{},
		}
	}

	// File too large
	recorder := httptest.NewRecorder()
	var resp interface{}
	_, _, ok := stageUploadedSaveFile(recorder, savesDir, 100, newFileHeader("big.zip", 200), &resp)
	assert.False(t, ok)
	assert.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code)

	// Invalid path
	recorder = httptest.NewRecorder()
	_, _, ok = stageUploadedSaveFile(recorder, savesDir, 100, newFileHeader("../evil.zip", 10), &resp)
	assert.False(t, ok)
	assert.Equal(t, http.StatusBadRequest, recorder.Code)

	// Invalid extension
	recorder = httptest.NewRecorder()
	_, _, ok = stageUploadedSaveFile(recorder, savesDir, 100, newFileHeader("test.txt", 10), &resp)
	assert.False(t, ok)
	assert.Equal(t, http.StatusUnsupportedMediaType, recorder.Code)

	// Temporary zip suffix not allowed
	recorder = httptest.NewRecorder()
	_, _, ok = stageUploadedSaveFile(recorder, savesDir, 100, newFileHeader("test.tmp.zip", 10), &resp)
	assert.False(t, ok)
	assert.Equal(t, http.StatusUnsupportedMediaType, recorder.Code)
}

func TestStageUploadedSaveFileHappyPath(t *testing.T) {
	savesDir := t.TempDir()

	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	part, err := writer.CreateFormFile("savefile", "test.zip")
	require.NoError(t, err)
	_, err = part.Write([]byte("fake zip contents"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	request := httptest.NewRequest(http.MethodPost, "/api/saves/upload", &buffer)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	err = request.ParseMultipartForm(32 << 20)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, request.MultipartForm.RemoveAll()) })

	fileHeader := request.MultipartForm.File["savefile"][0]

	var resp interface{}
	recorder := httptest.NewRecorder()
	file, tempPath, ok := stageUploadedSaveFile(recorder, savesDir, 1024*1024, fileHeader, &resp)
	require.True(t, ok)
	t.Cleanup(func() { _ = file.Close(); _ = os.Remove(tempPath) })
	assert.Equal(t, savesDir, filepath.Dir(tempPath))
	contents, err := os.ReadFile(tempPath)
	require.NoError(t, err)
	assert.Equal(t, "fake zip contents", string(contents))
	_, err = file.Seek(0, io.SeekStart)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, recorder.Code) // no error response written
}

func TestUploadSavePreservesMultipartParseErrorBody(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/saves/upload", strings.NewReader("invalid multipart"))
	request.Header.Set("Content-Type", "multipart/form-data; boundary=test")
	recorder := httptest.NewRecorder()
	UploadSave(recorder, request)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	var response string
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	assert.Equal(t, "Unable to parse save upload: multipart: NextPart: EOF", response)
}

func TestLoginUserInvalidJSONErrorMessage(t *testing.T) {
	setupLoginSecurityTest(t)
	recorder := runLoginRequest(`not valid json`, "192.0.2.1:1000")
	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	// WriteResponse JSON-encodes the string response.
	assert.Equal(t, `"Unable to parse login request"`, strings.TrimSpace(recorder.Body.String()))
}

func TestStartServerErrorStatus(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want int
	}{
		{"active", factorio.ErrServerActive, http.StatusConflict},
		{"world generation", fmt.Errorf("wrapped: %w", factorio.ErrWorldGenerationBusy), http.StatusConflict},
		{"configuration", factorio.ErrInvalidServerConfiguration, http.StatusBadRequest},
		{"unexpected", errors.New("start failed"), http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, startServerErrorStatus(tc.err))
		})
	}
}

func TestStartupExitResponse(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want int
	}{
		{"clean exit", nil, http.StatusInternalServerError},
		{"world generation", fmt.Errorf("wrapped: %w", factorio.ErrWorldGenerationBusy), http.StatusConflict},
		{"active", factorio.ErrServerActive, http.StatusInternalServerError},
		{"configuration", factorio.ErrInvalidServerConfiguration, http.StatusInternalServerError},
		{"unexpected", errors.New("early exit"), http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			message, status := startupExitResponse(tc.err)
			assert.Equal(t, tc.want, status)
			if tc.err == nil {
				assert.Equal(t, "Factorio server exited before startup completed", message)
			} else {
				assert.Equal(t, "Error starting Factorio server: "+tc.err.Error(), message)
			}
		})
	}
}
