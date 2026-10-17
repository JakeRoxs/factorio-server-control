package lockfile

import (
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFileLockCanonicalPath(t *testing.T) {
	lock := NewLock()
	directory := t.TempDir()
	path := filepath.Join(directory, "save.zip")
	alias := directory + string(filepath.Separator) + "unused" + string(filepath.Separator) + ".." + string(filepath.Separator) + "save.zip"
	require.NoError(t, lock.Lock(path))
	require.ErrorIs(t, lock.RLock(alias), ErrorAlreadyLocked)
	require.ErrorIs(t, lock.Lock(alias), ErrorAlreadyLocked)
	require.NoError(t, lock.Unlock(alias))
	require.NoError(t, lock.RLock(path))
	require.NoError(t, lock.RUnlock(path))
}

func TestFileLockReleasesIdleEntries(t *testing.T) {
	lock := NewLock()
	path := filepath.Join(t.TempDir(), "save.zip")
	require.NoError(t, lock.RLock(path))
	require.NoError(t, lock.RLock(path))
	require.ErrorIs(t, lock.Lock(path), ErrorAlreadyLocked)
	require.NoError(t, lock.RUnlock(path))
	require.ErrorIs(t, lock.Lock(path), ErrorAlreadyLocked)
	require.NoError(t, lock.RUnlock(path))
	require.Empty(t, lock.Locks)
	require.NoError(t, lock.Lock(path))
	require.ErrorIs(t, lock.RLock(path), ErrorAlreadyLocked)
	require.NoError(t, lock.Unlock(path))
	require.Empty(t, lock.Locks)
	require.Error(t, lock.Unlock(path))
	require.Error(t, lock.RUnlock(path))
	require.Empty(t, lock.Locks)
}

func TestFileLockConcurrentReaders(t *testing.T) {
	lock := NewLock()
	path := filepath.Join(t.TempDir(), strings.Repeat("s", 10)+".zip")
	var workers sync.WaitGroup
	for index := 0; index < 20; index++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for iteration := 0; iteration < 50; iteration++ {
				if err := lock.RLock(path); err != nil {
					t.Error(err)
					return
				}
				if err := lock.RUnlock(path); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	workers.Wait()
	require.Empty(t, lock.Locks)
}
