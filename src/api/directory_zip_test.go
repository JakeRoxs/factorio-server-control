package api

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/OpenFactorioServerManager/factorio-server-manager/factorio"
	"github.com/stretchr/testify/require"
)

func TestDirectoryZipPreservesNestedPaths(t *testing.T) {
	directory := t.TempDir()
	want := map[string]string{"mod-list.json": "root", "first/info.json": "first", "second/info.json": "second"}
	for name, contents := range want {
		path := filepath.Join(directory, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
		require.NoError(t, os.WriteFile(path, []byte(contents), 0600))
	}
	recorder := httptest.NewRecorder()
	require.NoError(t, serveDirectoryZip(recorder, directory, "all_mods.zip", true))
	require.Equal(t, "application/zip", recorder.Header().Get("Content-Type"))
	reader, err := zip.NewReader(bytes.NewReader(recorder.Body.Bytes()), int64(recorder.Body.Len()))
	require.NoError(t, err)
	got := map[string]string{}
	for _, entry := range reader.File {
		_, duplicate := got[entry.Name]
		require.False(t, duplicate, "duplicate archive path %q", entry.Name)
		stream, err := entry.Open()
		require.NoError(t, err)
		contents, err := io.ReadAll(stream)
		require.NoError(t, err)
		require.NoError(t, stream.Close())
		got[entry.Name] = string(contents)
	}
	require.Equal(t, want, got)
	for name := range want {
		path := filepath.Join(directory, filepath.FromSlash(name))
		require.NoError(t, factorio.FileLock.Lock(path), "archive must release its read locks")
		require.NoError(t, factorio.FileLock.Unlock(path))
	}
}

func TestDirectoryZipFailureDoesNotCommitResponse(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "locked.zip")
	require.NoError(t, os.WriteFile(path, []byte("not readable while locked"), 0600))
	require.NoError(t, factorio.FileLock.Lock(path))
	defer factorio.FileLock.Unlock(path)
	recorder := httptest.NewRecorder()
	require.Error(t, serveDirectoryZip(recorder, directory, "mods.zip", true))
	require.Empty(t, recorder.Body.String())
	require.Empty(t, recorder.Header().Get("Content-Type"))
	require.Empty(t, recorder.Header().Get("Content-Disposition"))
}

func TestDirectoryZipRejectsSymlinks(t *testing.T) {
	directory := t.TempDir()
	outside := filepath.Join(t.TempDir(), "private.txt")
	require.NoError(t, os.WriteFile(outside, []byte("must not be archived"), 0600))
	if err := os.Symlink(outside, filepath.Join(directory, "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	recorder := httptest.NewRecorder()
	require.Error(t, serveDirectoryZip(recorder, directory, "mods.zip", false))
	require.Zero(t, recorder.Body.Len())
}
