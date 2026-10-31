package api

import (
	"bytes"
	"encoding/json"
	"errors"
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

func TestModPackUploadRemovesSpilledMultipartFileOnValidationError(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("mod_file", "CON.zip")
	require.NoError(t, err)
	_, err = part.Write(bytes.Repeat([]byte("x"), 128))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	request := httptest.NewRequest(http.MethodPost, "/", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	// Preparse with a tiny memory allowance to exercise disk cleanup cheaply.
	require.NoError(t, request.ParseMultipartForm(1))
	t.Cleanup(func() { require.NoError(t, request.MultipartForm.RemoveAll()) })
	file, err := request.MultipartForm.File["mod_file"][0].Open()
	require.NoError(t, err)
	diskFile, ok := file.(*os.File)
	require.True(t, ok)
	path := diskFile.Name()
	require.NoError(t, file.Close())
	recorder := httptest.NewRecorder()
	ModPackModUploadHandler(recorder, request)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	_, err = os.Stat(path)
	assert.True(t, os.IsNotExist(err), "spilled upload must be removed before returning")
}

func TestModUploadParseErrorsPreserveRouteMessages(t *testing.T) {
	for _, tc := range []struct {
		name    string
		handler http.HandlerFunc
		prefix  string
	}{
		{"mods", ModUploadHandler, "error parsing mod upload: "},
		{"pack", ModPackModUploadHandler, "error parsing mod-pack upload: "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("not multipart"))
			request.Header.Set("Content-Type", "multipart/form-data; boundary=test")
			recorder := httptest.NewRecorder()
			tc.handler(recorder, request)
			require.Equal(t, http.StatusBadRequest, recorder.Code)
			assert.Equal(t, "application/json;charset=UTF-8", recorder.Header().Get("Content-Type"))
			var response string
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
			assert.Equal(t, tc.prefix+"multipart: NextPart: EOF", response)
		})
	}
}

func TestParseModUploadRejectsExceededBodyLimit(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("body exceeds limit"))
	request.Header.Set("Content-Type", "multipart/form-data; boundary=test")
	recorder := httptest.NewRecorder()
	response, err := parseModUpload(recorder, request, 1, "mod upload")
	var maximum *http.MaxBytesError
	require.ErrorAs(t, err, &maximum)
	assert.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code)
	assert.Contains(t, response, "error parsing mod upload: ")
}

func TestDownloadModPortalVersion(t *testing.T) {
	wanted := factorio.Version{2, 1, 3, 0}
	details := factorio.ModPortalStruct{Name: "fixture", Releases: []factorio.ModPortalRelease{
		{Version: factorio.Version{1, 0, 0, 0}, DownloadURL: "/older", FileName: "older.zip"},
		{Version: wanted, DownloadURL: "/selected", FileName: "selected.zip"},
		{Version: wanted, DownloadURL: "/duplicate", FileName: "duplicate.zip"},
	}}
	t.Run("first exact release only", func(t *testing.T) {
		calls := 0
		found, err := downloadModPortalVersion(details, wanted, func(url, filename, name string) error {
			calls++
			assert.Equal(t, "/selected", url)
			assert.Equal(t, "selected.zip", filename)
			assert.Equal(t, "fixture", name)
			return nil
		})
		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, 1, calls)
	})
	t.Run("missing version never downloads", func(t *testing.T) {
		found, err := downloadModPortalVersion(details, factorio.Version{9, 0, 0, 0}, func(string, string, string) error {
			t.Fatal("unexpected download")
			return nil
		})
		require.NoError(t, err)
		assert.False(t, found)
	})
	t.Run("download failure is returned", func(t *testing.T) {
		failure := errors.New("synthetic download failure")
		found, err := downloadModPortalVersion(details, wanted, func(string, string, string) error { return failure })
		assert.True(t, found)
		assert.ErrorIs(t, err, failure)
	})
}

func TestInstallModPortalVersionRejectsInvalidNameWithoutNetwork(t *testing.T) {
	recorder := httptest.NewRecorder()
	message, ok := installModPortalVersion(recorder, &factorio.Mods{}, "../invalid", factorio.Version{})
	assert.False(t, ok)
	assert.Equal(t, http.StatusInternalServerError, recorder.Code)
	assert.True(t, strings.HasPrefix(message, "Error in getting mod details from mod portal: "))
}

func TestSharedMultipartUploadFixture(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fixture.zip")
	require.NoError(t, os.WriteFile(path, []byte("synthetic upload"), 0600))
	body, contentType := buildMultipartUploadRequest(t, true, path)
	request := httptest.NewRequest(http.MethodPost, "/", body)
	request.Header.Set("Content-Type", contentType)
	require.NoError(t, request.ParseMultipartForm(1024))
	defer request.MultipartForm.RemoveAll()
	file, header, err := request.FormFile("mod_file")
	require.NoError(t, err)
	defer file.Close()
	assert.Equal(t, "fixture.zip", header.Filename)
	contents, err := io.ReadAll(file)
	require.NoError(t, err)
	assert.Equal(t, "synthetic upload", string(contents))
}

func TestModPackStateAssertionFixture(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		packMap := factorio.ModPackMap{"fixture": &factorio.ModPack{Mods: factorio.Mods{
			ModSimpleList: factorio.ModSimpleList{Mods: []factorio.ModSimple{
				{Name: "other", Enabled: !enabled},
				{Name: "target", Enabled: enabled},
			}},
		}}}
		assertModPackModState(t, packMap, "fixture", "target", enabled)
	}
}

func TestModHandlersPreserveJSONHeadersOnInvalidBody(t *testing.T) {
	for _, tc := range []struct {
		name    string
		handler http.HandlerFunc
	}{
		{"mod toggle", ModToggleHandler},
		{"portal batch install", ModPortalInstallMultipleHandler},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("invalid JSON"))
			recorder := httptest.NewRecorder()
			tc.handler(recorder, request)
			assert.Equal(t, http.StatusBadRequest, recorder.Code)
			assert.Equal(t, "application/json;charset=UTF-8", recorder.Header().Get("Content-Type"))
			var response string
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
			assert.NotEmpty(t, response)
		})
	}
}
