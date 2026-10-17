package factorio

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestStopAlreadyStoppedDoesNotReserveRuntime(t *testing.T) {
	server := &Server{}
	require.NoError(t, server.Stop())
	require.False(t, server.IsBusy(), "a racing stop after process exit must not lock the stopped runtime")
}

func TestFailedStartupClearsQueuedStopAndReleasesGuards(t *testing.T) {
	server := &Server{Settings: map[string]interface{}{"name": "isolated failure"}}
	claimed := make(chan struct{})
	release := make(chan struct{})
	originalHook := serverBeforeProcessStart
	serverBeforeProcessStart = func(serverStartSnapshot) { close(claimed); <-release }
	t.Cleanup(func() { serverBeforeProcessStart = originalHook })
	result, err := server.Start("127.0.0.1", 34197, "missing.zip")
	require.NoError(t, err)
	select {
	case <-claimed:
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("startup did not reach the barrier")
	}
	stopErr := server.Stop()
	close(release)
	select {
	case err := <-result:
		require.Error(t, err) // No configured engine or settings destination in this isolated test.
		// The result is buffered; guard cleanup completes before channel closure.
		select {
		case _, open := <-result:
			require.False(t, open)
		case <-time.After(2 * time.Second):
			t.Fatal("startup did not release its guards")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("failed startup did not finish")
	}
	require.NoError(t, stopErr)
	require.False(t, server.IsBusy(), "a failed start must not retain a queued stopping state")
	require.True(t, worldGenerationLock.TryLock(), "startup leaked the world-generation guard")
	worldGenerationLock.Unlock()
	require.True(t, profileDataGate.TryLock(), "startup leaked the profile-data guard")
	profileDataGate.Unlock()
}

func TestInvalidStartupSettingsDoNotChangeSelectionOrReserveRuntime(t *testing.T) {
	server := &Server{Savefile: "previous.zip", Settings: map[string]interface{}{"invalid": make(chan int)}}
	result, err := server.Start("127.0.0.1", 34197, "requested.zip")
	require.Nil(t, result)
	require.ErrorContains(t, err, "marshal Factorio server settings")
	require.Equal(t, "previous.zip", server.Snapshot().Savefile)
	require.False(t, server.IsBusy())
	require.True(t, worldGenerationLock.TryLock())
	worldGenerationLock.Unlock()
	require.True(t, profileDataGate.TryLock())
	profileDataGate.Unlock()
}
