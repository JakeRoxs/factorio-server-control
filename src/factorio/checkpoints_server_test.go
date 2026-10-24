package factorio

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// perServerSnapshotPaths records the paths the per-server live-save stub was
// asked to write, for assertions that the running-server branch targets the
// named server's saves directory.
var checkpointTestSnapshotPaths []string

func checkpointTestServer(t *testing.T, root, id, savefile string) *Server {
	t.Helper()
	saves := filepath.Join(root, id, "saves")
	mods := filepath.Join(root, id, "mods")
	config := filepath.Join(root, id, "config")
	for _, directory := range []string{saves, mods, config} {
		require.NoError(t, os.MkdirAll(directory, 0755))
	}
	writeCheckpointTestZip(t, filepath.Join(saves, savefile), "per-server saved world")
	return &Server{
		ID:        id,
		Name:      id,
		SavesDir:  saves,
		ModsDir:   mods,
		ConfigDir: config,
		Savefile:  savefile,
	}
}

// stubPerServerLiveSave writes the snapshot directly into the named server's
// saves directory, exercising the running-server branch's per-server target.
func stubPerServerLiveSave(t *testing.T, server *Server) {
	t.Helper()
	checkpointCreateLiveSaveForServer = func(server *Server, id string) (string, error) {
		path := filepath.Join(server.SavesDir, "synthetic-"+server.ID+".zip")
		writeCheckpointTestZip(t, path, "per-server snapshot")
		checkpointTestSnapshotPaths = append(checkpointTestSnapshotPaths, path)
		return path, nil
	}
	t.Cleanup(func() {
		checkpointCreateLiveSaveForServer = createLiveCheckpointSaveForServer
		checkpointTestSnapshotPaths = nil
	})
}

// TestPerServerCheckpointSourceTargetsServerSavesDir verifies that a stopped
// server's checkpoint snapshot is read from and written into that server's own
// saves directory, never another server's.
func TestPerServerCheckpointSourceTargetsServerSavesDir(t *testing.T) {
	setupCheckpointTest(t)
	require.NoError(t, InitializeProfiles())

	root := t.TempDir()
	serverA := checkpointTestServer(t, root, "a", "world-a.zip")
	serverB := checkpointTestServer(t, root, "b", "world-b.zip")

	stateA, err := CreateCheckpointForServer(serverA, CheckpointTriggerManual)
	require.NoError(t, err)
	_, err = CreateCheckpointForServer(serverB, CheckpointTriggerManual)
	require.NoError(t, err)

	// A's own save lives in A's directory and B's directory is untouched.
	_, statErr := os.Stat(filepath.Join(serverA.SavesDir, "world-a.zip"))
	require.NoError(t, statErr)
	_, statErr = os.Stat(filepath.Join(serverB.SavesDir, "world-b.zip"))
	require.NoError(t, statErr)
	_, statErr = os.Stat(filepath.Join(serverA.SavesDir, "world-b.zip"))
	assert.True(t, os.IsNotExist(statErr), "A should not see B's save")
	_, statErr = os.Stat(filepath.Join(serverB.SavesDir, "world-a.zip"))
	assert.True(t, os.IsNotExist(statErr), "B should not see A's save")

	// Restoring A must write into A's save directory only.
	restored, err := RestoreCheckpointForServer(serverA, stateA.Checkpoints[0].ID)
	require.NoError(t, err)
	assert.Contains(t, restored.Name, "restored-")
	_, statErr = os.Stat(filepath.Join(serverA.SavesDir, restored.Name))
	require.NoError(t, statErr, "restored save must land in A's saves directory")
	_, statErr = os.Stat(filepath.Join(serverB.SavesDir, restored.Name))
	assert.True(t, os.IsNotExist(statErr), "B's saves directory must not receive A's restore")
}

// TestPerServerCheckpointRunningTargetsServerSavesDir verifies the running-server
// branch binds the snapshot to that server's saves directory rather than the
// process-wide default runtime's.
func TestPerServerCheckpointRunningTargetsServerSavesDir(t *testing.T) {
	setupCheckpointTest(t)
	require.NoError(t, InitializeProfiles())

	root := t.TempDir()
	server := checkpointTestServer(t, root, "a", "world-a.zip")
	server.Running = true
	server.Stopping = false

	stubPerServerLiveSave(t, server)

	state, err := CreateCheckpointForServer(server, CheckpointTriggerManual)
	require.NoError(t, err)
	require.Len(t, state.Checkpoints, 1)
	require.Len(t, checkpointTestSnapshotPaths, 1, "the running-server branch must write the snapshot exactly once")
	require.True(t, filepath.Dir(checkpointTestSnapshotPaths[0]) == server.SavesDir, "running snapshot must target the named server's saves directory")
	require.Equal(t, "synthetic-a.zip", filepath.Base(checkpointTestSnapshotPaths[0]))
}

// TestPerServerCheckpointRestoreStoppedGuard verifies restore fail-closes for a
// running server and lands the restored save in the named server's directory.
func TestPerServerCheckpointRestoreStoppedGuard(t *testing.T) {
	setupCheckpointTest(t)
	require.NoError(t, InitializeProfiles())

	root := t.TempDir()
	server := checkpointTestServer(t, root, "a", "world-a.zip")
	state, err := CreateCheckpointForServer(server, CheckpointTriggerManual)
	require.NoError(t, err)
	require.Len(t, state.Checkpoints, 1)

	// A running server must fail closed on restore.
	server.Running = true
	server.Stopping = false
	_, err = RestoreCheckpointForServer(server, state.Checkpoints[0].ID)
	assert.ErrorIs(t, err, ErrCheckpointServerActive)

	server.Running = false
	server.Stopping = false
	restored, err := RestoreCheckpointForServer(server, state.Checkpoints[0].ID)
	require.NoError(t, err)
	assert.Contains(t, restored.Name, "restored-")
	_, statErr := os.Stat(filepath.Join(server.SavesDir, restored.Name))
	require.NoError(t, statErr)
}

// TestPerServerCheckpointIsolationPreservesServerStores verifies that creating
// checkpoints for two servers keeps their archive stores, settings and save
// sources independent, and that the persisted archives are never deleted.
func TestPerServerCheckpointIsolationPreservesServerStores(t *testing.T) {
	setupCheckpointTest(t)
	require.NoError(t, InitializeProfiles())

	root := t.TempDir()
	serverA := checkpointTestServer(t, root, "a", "world-a.zip")
	serverB := checkpointTestServer(t, root, "b", "world-b.zip")

	_, err := CreateCheckpointForServer(serverA, CheckpointTriggerManual)
	require.NoError(t, err)
	_, err = CreateCheckpointForServer(serverB, CheckpointTriggerManual)
	require.NoError(t, err)

	// Each server owns its own checkpoint store: A sees only A's checkpoints and
	// B sees only B's. Persisted archives are never deleted by the other server.
	stateA, err := GetCheckpointStateForServer(serverA)
	require.NoError(t, err)
	require.Len(t, stateA.Checkpoints, 1, "server A must own exactly its own checkpoint")
	require.DirExists(t, checkpointServerDirectory(serverA.ID))

	stateB, err := GetCheckpointStateForServer(serverB)
	require.NoError(t, err)
	require.Len(t, stateB.Checkpoints, 1, "server B must own exactly its own checkpoint")
	require.DirExists(t, checkpointServerDirectory(serverB.ID))

	_, statErr := os.Stat(filepath.Join(serverA.SavesDir, "world-b.zip"))
	assert.True(t, os.IsNotExist(statErr), "A must not see B's save source")
	_, statErr = os.Stat(filepath.Join(serverB.SavesDir, "world-a.zip"))
	assert.True(t, os.IsNotExist(statErr), "B must not see A's save source")
}

// TestPerServerCheckpointSettingsAreIsolated verifies that checkpoint settings
// (interval, retention, triggers) saved for one server are never visible to or
// applied to another server.
func TestPerServerCheckpointSettingsAreIsolated(t *testing.T) {
	setupCheckpointTest(t)
	require.NoError(t, InitializeProfiles())

	root := t.TempDir()
	serverA := checkpointTestServer(t, root, "a", "world-a.zip")
	serverB := checkpointTestServer(t, root, "b", "world-b.zip")

	// Configure A with a custom schedule.
	settingsA := CheckpointSettings{
		IntervalEnabled:   true,
		IntervalMinutes:   45,
		LastPlayerEnabled: true,
		CleanStopEnabled:  false,
		RetentionCount:    5,
	}
	stateA, err := UpdateCheckpointSettingsForServer(serverA, settingsA)
	require.NoError(t, err)
	assert.Equal(t, settingsA, stateA.Settings)

	// B must still have the default settings, unaffected by A's update.
	stateB, err := GetCheckpointStateForServer(serverB)
	require.NoError(t, err)
	assert.Equal(t, defaultCheckpointSettings(), stateB.Settings, "B must not inherit A's settings")
	assert.False(t, stateB.Settings.IntervalEnabled)
	assert.Equal(t, defaultCheckpointIntervalMinutes, stateB.Settings.IntervalMinutes)
	assert.Equal(t, defaultCheckpointRetention, stateB.Settings.RetentionCount)

	// Update B with different settings.
	settingsB := CheckpointSettings{
		IntervalEnabled:   true,
		IntervalMinutes:   120,
		LastPlayerEnabled: false,
		CleanStopEnabled:  true,
		RetentionCount:    20,
	}
	stateB, err = UpdateCheckpointSettingsForServer(serverB, settingsB)
	require.NoError(t, err)
	assert.Equal(t, settingsB, stateB.Settings)

	// A must still have its original settings, unaffected by B's update.
	stateA, err = GetCheckpointStateForServer(serverA)
	require.NoError(t, err)
	assert.Equal(t, settingsA, stateA.Settings, "A must not inherit B's settings")

	// Verify persisted state files are distinct.
	stateFileA := filepath.Join(checkpointServerDirectory(serverA.ID), checkpointStateFile)
	stateFileB := filepath.Join(checkpointServerDirectory(serverB.ID), checkpointStateFile)
	require.FileExists(t, stateFileA)
	require.FileExists(t, stateFileB)
	assert.NotEqual(t, stateFileA, stateFileB)
}

// TestLegacyCheckpointMigrationPreservesAllArchives verifies that legacy
// profile-bound checkpoint stores are migrated into the default server's
// directory without losing any archives or metadata.
func TestLegacyCheckpointMigrationPreservesAllArchives(t *testing.T) {
	environment := setupCheckpointTest(t)
	_ = environment
	require.NoError(t, InitializeProfiles())

	// Create a legacy profile-bound checkpoint store manually.
	legacyProfileID := "legacy-profile-abc"
	legacyDir := filepath.Join(checkpointRootPath(), legacyProfileID, checkpointFilesDir)
	require.NoError(t, os.MkdirAll(legacyDir, 0700))
	legacyCheckpointID := "20260101T000000.000000000Z-manual"
	legacyZipPath := filepath.Join(legacyDir, legacyCheckpointID+".zip")
	legacyMetaPath := filepath.Join(legacyDir, legacyCheckpointID+".json")
	writeCheckpointTestZip(t, legacyZipPath, "legacy world")
	metaJSON := `{"schema_version":1,"id":"` + legacyCheckpointID + `","created_at":"2026-01-01T00:00:00Z","trigger":"manual","source_save":"legacy.zip"}`
	require.NoError(t, os.WriteFile(legacyMetaPath, []byte(metaJSON), 0600))

	// Write a legacy state file.
	legacyStatePath := filepath.Join(checkpointRootPath(), legacyProfileID, checkpointStateFile)
	require.NoError(t, os.WriteFile(legacyStatePath, []byte(`{"schema_version":1,"settings":{"interval_minutes":30,"retention_count":10}}`), 0600))

	// Run migration.
	require.NoError(t, MigrateLegacyProfileCheckpoints())

	// The default server must now have the migrated checkpoint.
	defaultServer := GetFactorioServer()
	state, err := GetCheckpointStateForServer(defaultServer)
	require.NoError(t, err)
	require.Len(t, state.Checkpoints, 1)
	assert.Equal(t, legacyCheckpointID, state.Checkpoints[0].ID)

	// The legacy source directory should be empty (files moved).
	entries, err := os.ReadDir(legacyDir)
	require.NoError(t, err)
	assert.Empty(t, entries, "legacy files should be moved, not copied")
}

// TestLegacyCheckpointMigrationIsIdempotent verifies that running migration
// multiple times does not duplicate data or fail.
func TestLegacyCheckpointMigrationIsIdempotent(t *testing.T) {
	environment := setupCheckpointTest(t)
	_ = environment
	require.NoError(t, InitializeProfiles())

	// Create a legacy profile-bound checkpoint store manually.
	legacyProfileID := "legacy-profile-idem"
	legacyDir := filepath.Join(checkpointRootPath(), legacyProfileID, checkpointFilesDir)
	require.NoError(t, os.MkdirAll(legacyDir, 0700))
	legacyCheckpointID := "20260101T000000.000000000Z-manual"
	legacyZipPath := filepath.Join(legacyDir, legacyCheckpointID+".zip")
	writeCheckpointTestZip(t, legacyZipPath, "legacy world")

	// Run migration twice.
	require.NoError(t, MigrateLegacyProfileCheckpoints())
	require.NoError(t, MigrateLegacyProfileCheckpoints())

	// The default server must have exactly one checkpoint (no duplication).
	defaultServer := GetFactorioServer()
	state, err := GetCheckpointStateForServer(defaultServer)
	require.NoError(t, err)
	require.Len(t, state.Checkpoints, 1)
	assert.Equal(t, legacyCheckpointID, state.Checkpoints[0].ID)
}

// TestLegacyCheckpointMigrationPreservesExistingDefaultState verifies that if
// the default server already has checkpoint state, migration does not overwrite
// it or duplicate its archives.
func TestLegacyCheckpointMigrationPreservesExistingDefaultState(t *testing.T) {
	environment := setupCheckpointTest(t)
	_ = environment
	require.NoError(t, InitializeProfiles())

	defaultServer := GetFactorioServer()

	// Create a checkpoint on the default server first.
	defaultState, err := CreateCheckpointForServer(defaultServer, CheckpointTriggerManual)
	require.NoError(t, err)
	require.Len(t, defaultState.Checkpoints, 1)
	defaultCheckpointID := defaultState.Checkpoints[0].ID

	// Create a legacy profile-bound checkpoint store.
	legacyProfileID := "legacy-profile-existing"
	legacyDir := filepath.Join(checkpointRootPath(), legacyProfileID, checkpointFilesDir)
	require.NoError(t, os.MkdirAll(legacyDir, 0700))
	legacyCheckpointID := "20251231T235959.000000000Z-manual"
	legacyZipPath := filepath.Join(legacyDir, legacyCheckpointID+".zip")
	writeCheckpointTestZip(t, legacyZipPath, "legacy world")

	// Run migration.
	require.NoError(t, MigrateLegacyProfileCheckpoints())

	// The default server must have both checkpoints.
	state, err := GetCheckpointStateForServer(defaultServer)
	require.NoError(t, err)
	require.Len(t, state.Checkpoints, 2)
	ids := make(map[string]bool)
	for _, cp := range state.Checkpoints {
		ids[cp.ID] = true
	}
	assert.True(t, ids[defaultCheckpointID], "original default checkpoint must be preserved")
	assert.True(t, ids[legacyCheckpointID], "legacy checkpoint must be migrated")
}

// TestLegacyCheckpointMigrationDuplicateIDsPreservesBoth verifies that when
// legacy stores contain checkpoints with duplicate IDs, migration does not
// silently discard archives.
func TestLegacyCheckpointMigrationDuplicateIDsPreservesBoth(t *testing.T) {
	environment := setupCheckpointTest(t)
	_ = environment
	require.NoError(t, InitializeProfiles())

	// Create two legacy profile-bound stores with checkpoints that have the
	// same ID (same timestamp).
	legacyProfileA := "legacy-profile-dup-a"
	legacyProfileB := "legacy-profile-dup-b"
	dupCheckpointID := "20260101T000000.000000000Z-manual"

	for _, profileID := range []string{legacyProfileA, legacyProfileB} {
		legacyDir := filepath.Join(checkpointRootPath(), profileID, checkpointFilesDir)
		require.NoError(t, os.MkdirAll(legacyDir, 0700))
		legacyZipPath := filepath.Join(legacyDir, dupCheckpointID+".zip")
		writeCheckpointTestZip(t, legacyZipPath, "legacy world from "+profileID)
	}

	// Run migration.
	require.NoError(t, MigrateLegacyProfileCheckpoints())

	// The default server must have the checkpoint.
	defaultServer := GetFactorioServer()
	state, err := GetCheckpointStateForServer(defaultServer)
	require.NoError(t, err)
	require.Len(t, state.Checkpoints, 1)
	assert.Equal(t, dupCheckpointID, state.Checkpoints[0].ID)

	// One legacy directory should be empty (file moved), the other should
	// retain its file due to the collision (preserved, not discarded).
	moved := false
	preserved := false
	for _, profileID := range []string{legacyProfileA, legacyProfileB} {
		legacyDir := filepath.Join(checkpointRootPath(), profileID, checkpointFilesDir)
		entries, err := os.ReadDir(legacyDir)
		require.NoError(t, err)
		if len(entries) == 0 {
			moved = true
		} else {
			preserved = true
		}
	}
	assert.True(t, moved, "one legacy file should have been moved")
	assert.True(t, preserved, "the colliding legacy file should be preserved, not discarded")
}

// TestMigrationDoesNotStealPerServerStores is the critical regression test:
// migration must never move or touch per-server checkpoint stores created via
// the new architecture. It creates two per-server stores, runs migration, and
// verifies both stores are unchanged and their checkpoints are not exposed to
// the default server.
func TestMigrationDoesNotStealPerServerStores(t *testing.T) {
	setupCheckpointTest(t)
	require.NoError(t, InitializeProfiles())

	root := t.TempDir()
	serverA := checkpointTestServer(t, root, "srv-a", "world-a.zip")
	serverB := checkpointTestServer(t, root, "srv-b", "world-b.zip")

	// Create per-server checkpoints via the new architecture.
	stateA, err := CreateCheckpointForServer(serverA, CheckpointTriggerManual)
	require.NoError(t, err)
	require.Len(t, stateA.Checkpoints, 1)
	checkpointAID := stateA.Checkpoints[0].ID

	// Advance time so B gets a different ID.
	originalNow := checkpointNow
	checkpointNow = func() time.Time { return originalNow().Add(time.Second) }
	defer func() { checkpointNow = originalNow }()

	stateB, err := CreateCheckpointForServer(serverB, CheckpointTriggerManual)
	require.NoError(t, err)
	require.Len(t, stateB.Checkpoints, 1)
	checkpointBID := stateB.Checkpoints[0].ID

	// Run migration multiple times (simulating repeated startups).
	require.NoError(t, MigrateLegacyProfileCheckpoints())
	require.NoError(t, MigrateLegacyProfileCheckpoints())

	// Both per-server stores must be unchanged.
	stateAAfter, err := GetCheckpointStateForServer(serverA)
	require.NoError(t, err)
	require.Len(t, stateAAfter.Checkpoints, 1)
	assert.Equal(t, checkpointAID, stateAAfter.Checkpoints[0].ID)
	assert.Equal(t, checkpointAID, stateAAfter.Checkpoints[0].ID)

	stateBAfter, err := GetCheckpointStateForServer(serverB)
	require.NoError(t, err)
	require.Len(t, stateBAfter.Checkpoints, 1)
	assert.Equal(t, checkpointBID, stateBAfter.Checkpoints[0].ID)

	// The default server must not have been exposed to either per-server checkpoint.
	defaultServer := GetFactorioServer()
	defaultState, err := GetCheckpointStateForServer(defaultServer)
	require.NoError(t, err)
	for _, cp := range defaultState.Checkpoints {
		assert.NotEqual(t, checkpointAID, cp.ID, "default server must not see server A's checkpoint")
		assert.NotEqual(t, checkpointBID, cp.ID, "default server must not see server B's checkpoint")
	}

	// Verify the per-server store directories still exist and are intact.
	storeDirA := filepath.Join(checkpointRootPath(), checkpointServersSubdir, serverA.ID)
	storeDirB := filepath.Join(checkpointRootPath(), checkpointServersSubdir, serverB.ID)
	require.DirExists(t, storeDirA)
	require.DirExists(t, storeDirB)
}
