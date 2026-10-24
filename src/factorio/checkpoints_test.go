package factorio

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type checkpointTestEnvironment struct {
	profile *profileTestEnvironment
	now     time.Time
}

func setupCheckpointTest(t *testing.T) *checkpointTestEnvironment {
	t.Helper()
	profileEnvironment := setupProfileTest(t)
	require.NoError(t, InitializeProfiles())
	environment := &checkpointTestEnvironment{
		profile: profileEnvironment,
		now:     time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC),
	}
	writeCheckpointTestZip(t, filepath.Join(profileEnvironment.active["saves"], "current.zip"), "initial world")

	// Configure the default server with the test environment's directories so
	// per-server checkpoint operations target the correct saves location.
	defaultServer := *GetFactorioServer()
	defaultServer.ID = DefaultServerID
	defaultServer.SavesDir = profileEnvironment.active["saves"]
	defaultServer.ModsDir = profileEnvironment.active["mods"]
	defaultServer.ConfigDir = profileEnvironment.active["config"]
	defaultServer.Savefile = "current.zip"
	SetFactorioServer(defaultServer)

	originalRootPath := checkpointRootPath
	originalSavesPath := checkpointSavesPath
	originalNow := checkpointNow
	originalRunRCON := checkpointRunRCON
	originalCreateLiveSave := checkpointCreateLiveSave
	originalRunRCONForServer := checkpointRunRCONForServer
	originalCreateLiveSaveForServer := checkpointCreateLiveSaveForServer
	originalPollInterval := checkpointMonitorPollInterval
	originalCooldown := checkpointAutomatedCooldown
	checkpointRootPath = func() string { return filepath.Join(profileEnvironment.root, "checkpoints") }
	checkpointSavesPath = func() string { return profileEnvironment.active["saves"] }
	checkpointNow = func() time.Time { return environment.now }
	checkpointMonitorPollInterval = 15 * time.Second
	checkpointAutomatedCooldown = 2 * time.Minute

	t.Cleanup(func() {
		stopCheckpointMonitor("")
		checkpointRootPath = originalRootPath
		checkpointSavesPath = originalSavesPath
		checkpointNow = originalNow
		checkpointRunRCON = originalRunRCON
		checkpointCreateLiveSave = originalCreateLiveSave
		checkpointRunRCONForServer = originalRunRCONForServer
		checkpointCreateLiveSaveForServer = originalCreateLiveSaveForServer
		checkpointMonitorPollInterval = originalPollInterval
		checkpointAutomatedCooldown = originalCooldown
	})
	return environment
}

func TestCheckpointSettingsPersistPerActiveProfile(t *testing.T) {
	environment := setupCheckpointTest(t)
	settings := CheckpointSettings{
		IntervalEnabled:   true,
		IntervalMinutes:   45,
		LastPlayerEnabled: true,
		CleanStopEnabled:  true,
		RetentionCount:    7,
	}
	state, err := UpdateCheckpointSettings(settings)
	require.NoError(t, err)
	assert.Equal(t, settings, state.Settings)
	assert.Empty(t, state.Checkpoints)

	state, err = GetCheckpointState()
	require.NoError(t, err)
	assert.Equal(t, settings, state.Settings)
	// The default server's checkpoint store is under root/servers/{serverID}/.
	assert.FileExists(t, filepath.Join(environment.profile.root, "checkpoints", checkpointServersSubdir, DefaultServerID, checkpointStateFile))

	_, err = UpdateCheckpointSettings(CheckpointSettings{IntervalMinutes: 1, RetentionCount: 10})
	assert.ErrorIs(t, err, ErrInvalidCheckpointSetting)
}

func TestCheckpointCreationRetentionAndRestoreAreImmutable(t *testing.T) {
	environment := setupCheckpointTest(t)
	state, err := UpdateCheckpointSettings(CheckpointSettings{IntervalMinutes: 30, RetentionCount: 2})
	require.NoError(t, err)

	state, err = CreateCheckpoint(CheckpointTriggerManual)
	require.NoError(t, err)
	require.Len(t, state.Checkpoints, 1)
	oldest := state.Checkpoints[0]
	// The default server's checkpoint store is under root/servers/{serverID}/files/.
	oldestPath := filepath.Join(checkpointServerDirectory(DefaultServerID), checkpointFilesDir, oldest.FileName)
	assert.Equal(t, "initial world", readCheckpointTestZip(t, oldestPath))

	writeCheckpointTestZip(t, filepath.Join(environment.profile.active["saves"], "current.zip"), "second world")
	environment.now = environment.now.Add(time.Minute)
	_, err = CreateCheckpoint(CheckpointTriggerManual)
	require.NoError(t, err)
	assert.Equal(t, "initial world", readCheckpointTestZip(t, oldestPath), "creating a newer checkpoint must not rewrite the old archive")

	writeCheckpointTestZip(t, filepath.Join(environment.profile.active["saves"], "current.zip"), "third world")
	environment.now = environment.now.Add(time.Minute)
	state, err = CreateCheckpoint(CheckpointTriggerManual)
	require.NoError(t, err)
	require.Len(t, state.Checkpoints, 2)
	assert.NoFileExists(t, oldestPath, "retention removes the oldest archive only after the replacement exists")

	checkpointToRestore := state.Checkpoints[0]
	checkpointPath := filepath.Join(checkpointServerDirectory(DefaultServerID), checkpointFilesDir, checkpointToRestore.FileName)
	originalCheckpointContents := readCheckpointTestZip(t, checkpointPath)
	restored, err := RestoreCheckpoint(checkpointToRestore.ID)
	require.NoError(t, err)
	assert.Contains(t, restored.Name, "restored-")
	assert.Equal(t, originalCheckpointContents, readCheckpointTestZip(t, filepath.Join(environment.profile.active["saves"], restored.Name)))
	assert.Equal(t, originalCheckpointContents, readCheckpointTestZip(t, checkpointPath), "restore must leave the fixed checkpoint untouched")

	state, err = DeleteCheckpoint(checkpointToRestore.ID)
	require.NoError(t, err)
	require.Len(t, state.Checkpoints, 1)
	assert.NotNil(t, state.LastCheckpointAt)
	state, err = DeleteCheckpoint(state.Checkpoints[0].ID)
	require.NoError(t, err)
	assert.Empty(t, state.Checkpoints)
	assert.Nil(t, state.LastCheckpointAt, "deleting the final checkpoint must not suppress the next automatic trigger")
}

func TestCheckpointsRemainBoundToTheirServer(t *testing.T) {
	environment := setupCheckpointTest(t)
	require.NoError(t, InitializeProfiles())

	root := t.TempDir()
	serverA := checkpointTestServer(t, root, "a", "world-a.zip")
	serverB := checkpointTestServer(t, root, "b", "world-b.zip")

	// Create a checkpoint for server A.
	stateA, err := CreateCheckpointForServer(serverA, CheckpointTriggerManual)
	require.NoError(t, err)
	require.Len(t, stateA.Checkpoints, 1)
	aCheckpointID := stateA.Checkpoints[0].ID

	// Server B should have no checkpoints and no access to A's.
	stateB, err := GetCheckpointStateForServer(serverB)
	require.NoError(t, err)
	assert.Empty(t, stateB.Checkpoints, "server B must not see server A's checkpoints")

	// Advance time so B's checkpoint gets a different ID.
	checkpointNow = func() time.Time { return environment.now.Add(time.Second) }

	// Creating a checkpoint for B must not affect A's store.
	stateB, err = CreateCheckpointForServer(serverB, CheckpointTriggerManual)
	require.NoError(t, err)
	require.Len(t, stateB.Checkpoints, 1)
	assert.NotEqual(t, aCheckpointID, stateB.Checkpoints[0].ID)

	// A still has exactly its own checkpoint.
	stateA, err = GetCheckpointStateForServer(serverA)
	require.NoError(t, err)
	require.Len(t, stateA.Checkpoints, 1)
	assert.Equal(t, aCheckpointID, stateA.Checkpoints[0].ID)
}

func TestCheckpointMonitorUsesConfirmedPlayerTransition(t *testing.T) {
	environment := setupCheckpointTest(t)
	server := GetFactorioServer()
	_, err := UpdateCheckpointSettingsForServer(server, CheckpointSettings{
		IntervalMinutes:   30,
		LastPlayerEnabled: true,
		RetentionCount:    10,
	})
	require.NoError(t, err)
	checkpointMonitorPollInterval = 10 * time.Millisecond
	checkpointAutomatedCooldown = 0
	checkpointNow = time.Now

	var responseMutex sync.Mutex
	responses := []string{"Online players (1):\n", "Online players (0):\r\n", "Online players (0):\n"}
	checkpointRunRCONForServer = func(s *Server, command string) (string, error) {
		responseMutex.Lock()
		defer responseMutex.Unlock()
		assert.Equal(t, "/players online count", command)
		if len(responses) == 0 {
			return "Online players (0):\n", nil
		}
		response := responses[0]
		responses = responses[1:]
		return response, nil
	}
	checkpointCreateLiveSaveForServer = func(s *Server, id string) (string, error) {
		path := filepath.Join(environment.profile.active["saves"], fmt.Sprintf("live-%s.zip", id))
		writeCheckpointTestZip(t, path, "live world")
		return path, nil
	}

	server.SetRunning(true)
	startCheckpointMonitor(server)

	require.Eventually(t, func() bool {
		state, stateErr := GetCheckpointStateForServer(server)
		return stateErr == nil && len(state.Checkpoints) == 1 && state.Checkpoints[0].Trigger == CheckpointTriggerLastPlayer
	}, time.Second, 20*time.Millisecond)
	state, err := GetCheckpointStateForServer(server)
	require.NoError(t, err)
	require.Len(t, state.Checkpoints, 1, "repeated zero-player polls must not create duplicate checkpoints")

	// Stop the monitor before cleanup to avoid deadlock.
	stopCheckpointMonitor(server.ID)
	server.SetRunning(false)
}

func TestParseConnectedPlayerCount(t *testing.T) {
	tests := []struct {
		name     string
		response string
		want     int
	}{
		{name: "zero with newline", response: "Online players (0):\n", want: 0},
		{name: "multiple with CRLF", response: "Online players (12):\r\n", want: 12},
		{name: "case and whitespace", response: "  ONLINE PLAYERS ( 7 )  \r\n", want: 7},
		{name: "surrounding blank lines", response: "\r\nOnline players (3):\r\n\r\n", want: 3},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			count, err := parseConnectedPlayerCount(test.response)
			require.NoError(t, err)
			assert.Equal(t, test.want, count)
		})
	}
}

func TestParseConnectedPlayerCountRejectsUnexpectedResponses(t *testing.T) {
	responses := []string{
		"",
		"0",
		"Players (0):\n",
		"Online players (-1):\n",
		"Online players (1):\nOnline players (0):\n",
		"Unknown command: players online count\n",
		"Online players (999999999999999999999999999999):\n",
	}
	for _, response := range responses {
		t.Run(fmt.Sprintf("response_%q", response), func(t *testing.T) {
			_, err := parseConnectedPlayerCount(response)
			assert.Error(t, err)
		})
	}
}

func TestCleanStopCheckpointHonorsProfileSetting(t *testing.T) {
	environment := setupCheckpointTest(t)
	server := GetFactorioServer()
	_, err := UpdateCheckpointSettingsForServer(server, CheckpointSettings{
		IntervalMinutes:  30,
		CleanStopEnabled: true,
		RetentionCount:   10,
	})
	require.NoError(t, err)
	checkpointCreateLiveSaveForServer = func(s *Server, id string) (string, error) {
		path := filepath.Join(environment.profile.active["saves"], fmt.Sprintf("clean-stop-%s.zip", id))
		writeCheckpointTestZip(t, path, "clean stop world")
		return path, nil
	}

	server.SetRunning(true)
	runCleanStopCheckpoint(server)
	server.SetRunning(false)

	state, err := GetCheckpointStateForServer(server)
	require.NoError(t, err)
	require.Len(t, state.Checkpoints, 1)
	assert.Equal(t, CheckpointTriggerCleanStop, state.Checkpoints[0].Trigger)
}

func writeCheckpointTestZip(t *testing.T, path, contents string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
	file, err := os.Create(path)
	require.NoError(t, err)
	writer := zip.NewWriter(file)
	entry, err := writer.Create("world/level.dat")
	require.NoError(t, err)
	_, err = entry.Write([]byte(contents))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	require.NoError(t, file.Close())
}

func readCheckpointTestZip(t *testing.T, path string) string {
	t.Helper()
	reader, err := zip.OpenReader(path)
	require.NoError(t, err)
	defer reader.Close()
	require.NotEmpty(t, reader.File)
	entry, err := reader.File[0].Open()
	require.NoError(t, err)
	defer entry.Close()
	buffer, err := io.ReadAll(entry)
	require.NoError(t, err)
	return string(buffer)
}
