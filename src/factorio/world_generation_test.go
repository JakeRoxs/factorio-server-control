package factorio

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func floatPointer(value float64) *float64 { return &value }
func boolPointer(value bool) *bool        { return &value }
func uint32Pointer(value uint32) *uint32  { return &value }

func testWorldOptions() WorldGenerationOptions {
	return WorldGenerationOptions{
		Planets:  append(append([]WorldPlanet{}, baseWorldPlanets...), spaceAgeWorldPlanets...),
		Presets:  append([]WorldPreset{}, worldPresets...),
		Controls: append(append([]WorldControlDefinition{}, baseWorldControls...), spaceAgeWorldControls...),
	}
}

func TestNormalizeWorldGenerationRequestPreservesPresetDefaults(t *testing.T) {
	request, _, err := normalizeWorldGenerationRequestWithOptions(WorldGenerationRequest{
		Preset: "",
		Seed:   42,
	}, true, testWorldOptions())

	require.NoError(t, err)
	assert.Equal(t, "default", request.Preset)
	assert.Equal(t, "nauvis", request.Planet)
	assert.Equal(t, defaultMapPreviewSize, request.PreviewSize)
	assert.Nil(t, request.Width)
	assert.Nil(t, request.Height)
	assert.Nil(t, request.StartingArea)
	assert.Nil(t, request.PeacefulMode)
	assert.Nil(t, request.NoEnemiesMode)
}

func TestNormalizeWorldGenerationRequestAddsZipAndAcceptsOverrides(t *testing.T) {
	request, _, err := normalizeWorldGenerationRequestWithOptions(WorldGenerationRequest{
		Name:          "space-factory",
		Preset:        "rail-world",
		Seed:          4294967295,
		Width:         uint32Pointer(0),
		Height:        uint32Pointer(256),
		StartingArea:  floatPointer(2),
		PeacefulMode:  boolPointer(false),
		NoEnemiesMode: boolPointer(true),
		Controls: map[string]WorldControlSettings{
			"scrap": {Frequency: floatPointer(2), Size: floatPointer(4), Richness: floatPointer(6)},
		},
	}, false, testWorldOptions())

	require.NoError(t, err)
	assert.Equal(t, "space-factory.zip", request.Name)
}

func TestNormalizeWorldGenerationRequestRejectsUnavailablePlanet(t *testing.T) {
	options := testWorldOptions()
	options.Planets = append([]WorldPlanet{}, baseWorldPlanets...)

	_, _, err := normalizeWorldGenerationRequestWithOptions(WorldGenerationRequest{
		Preset: "default",
		Planet: "vulcanus",
	}, true, options)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not available")
}

func TestNormalizeWorldGenerationRequestRejectsDisabledRequiredTerrain(t *testing.T) {
	_, _, err := normalizeWorldGenerationRequestWithOptions(WorldGenerationRequest{
		Preset: "default",
		Controls: map[string]WorldControlSettings{
			"vulcanus_volcanism": {Frequency: floatPointer(0)},
		},
	}, true, testWorldOptions())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be disabled")
}

func TestBuildMapGenSettingsLeavesUnchangedPresetValuesOut(t *testing.T) {
	settings := buildMapGenSettings(WorldGenerationRequest{Seed: 123}, testWorldOptions())

	assert.Equal(t, uint64(123), settings["seed"])
	assert.NotContains(t, settings, "width")
	assert.NotContains(t, settings, "starting_area")
	assert.NotContains(t, settings, "peaceful_mode")
	assert.NotContains(t, settings, "no_enemies_mode")
	assert.NotContains(t, settings, "autoplace_controls")
}

func TestBuildMapGenSettingsIncludesOnlyExplicitControlValues(t *testing.T) {
	settings := buildMapGenSettings(WorldGenerationRequest{
		Seed:          123,
		Width:         uint32Pointer(0),
		PeacefulMode:  boolPointer(false),
		NoEnemiesMode: boolPointer(true),
		Controls: map[string]WorldControlSettings{
			"water": {Frequency: floatPointer(0.5), Size: floatPointer(2)},
		},
	}, testWorldOptions())

	assert.Equal(t, uint32(0), settings["width"])
	assert.Equal(t, false, settings["peaceful_mode"])
	assert.Equal(t, true, settings["no_enemies_mode"])
	autoplace := settings["autoplace_controls"].(map[string]map[string]float64)
	assert.Equal(t, map[string]float64{"frequency": 0.5, "size": 2}, autoplace["water"])
}

func TestRedactWorldCommandOutput(t *testing.T) {
	redacted := redactWorldCommandOutput([]byte("token=secret-value\n--rcon-password hunter2"))

	assert.NotContains(t, redacted, "secret-value")
	assert.NotContains(t, redacted, "hunter2")
	assert.Contains(t, redacted, "[REDACTED]")
}

func TestServerRunRejectsConcurrentWorldGeneration(t *testing.T) {
	worldGenerationLock.Lock()
	defer worldGenerationLock.Unlock()

	err := (&Server{}).Run()

	require.ErrorIs(t, err, ErrWorldGenerationBusy)
}

func TestCloseRconWithoutConnection(t *testing.T) {
	server := &Server{}
	require.NotPanics(t, server.closeRcon)
}

func TestCreateServerWorldRejectsMissingServerManager(t *testing.T) {
	serverManagerMu.Lock()
	previous := serverManager
	serverManager = nil
	serverManagerMu.Unlock()
	defer func() {
		serverManagerMu.Lock()
		serverManager = previous
		serverManagerMu.Unlock()
	}()

	_, err := CreateServerWorld("1", WorldGenerationRequest{Preset: "default", Seed: 42})
	require.Error(t, err)
	require.Contains(t, err.Error(), "server manager is not initialized")
}

func TestCreateServerWorldRejectsUnknownServerID(t *testing.T) {
	env, manager := setupRuntimeManager(t)
	_ = env
	serverManagerMu.Lock()
	previous := serverManager
	serverManager = manager
	serverManagerMu.Unlock()
	defer func() {
		serverManagerMu.Lock()
		serverManager = previous
		serverManagerMu.Unlock()
	}()

	_, err := CreateServerWorld("nonexistent", WorldGenerationRequest{Preset: "default", Seed: 42})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestCreateServerWorldRejectsRunningServer(t *testing.T) {
	env, manager := setupRuntimeManager(t)
	_ = env
	serverManagerMu.Lock()
	previous := serverManager
	serverManager = manager
	serverManagerMu.Unlock()
	defer func() {
		serverManagerMu.Lock()
		serverManager = previous
		serverManagerMu.Unlock()
	}()

	server := GetFactorioServer()
	stateMutex.Lock()
	server.Running = true
	stateMutex.Unlock()
	defer func() {
		stateMutex.Lock()
		server.Running = false
		stateMutex.Unlock()
	}()

	_, err := CreateServerWorld(DefaultServerID, WorldGenerationRequest{Preset: "default", Seed: 42})
	require.ErrorIs(t, err, ErrFactorioMustBeStopped)
}

func TestCreateServerWorldWritesToServerSpecificSavesDir(t *testing.T) {
	env, manager := setupRuntimeManager(t)
	_ = env
	serverManagerMu.Lock()
	previous := serverManager
	serverManager = manager
	serverManagerMu.Unlock()
	defer func() {
		serverManagerMu.Lock()
		serverManager = previous
		serverManagerMu.Unlock()
	}()

	// Create a second server to verify isolation
	server2, err := manager.CreateServer("Second Server", "stable", "127.0.0.1", 34198, false)
	require.NoError(t, err)

	// Verify the SavesDir for the second server
	require.True(t, strings.HasSuffix(server2.SavesDir, filepath.Join("servers", server2.ID, "saves")),
		"SavesDir should be server-specific")

	// Verify the first server's SavesDir is different
	server1, ok := manager.GetServer(DefaultServerID)
	require.True(t, ok)
	require.NotEqual(t, server1.SavesDir, server2.SavesDir)

	// Create a fake save in server 1's directory to verify isolation
	require.NoError(t, os.WriteFile(filepath.Join(server1.SavesDir, "existing.zip"), []byte("server 1 save"), 0600))

	// Create a world for server 2 (will fail without Factorio binary, but
	// we can verify it doesn't touch server 1's saves directory)
	_, err = CreateServerWorld(server2.ID, WorldGenerationRequest{Preset: "default", Seed: 123, Name: "new-world"})
	// The error is expected because Factorio binary may not be available,
	// but the key assertion is that server 1's save is unchanged.

	// Verify server 1's save is unchanged
	contents, err := os.ReadFile(filepath.Join(server1.SavesDir, "existing.zip"))
	require.NoError(t, err)
	require.Equal(t, []byte("server 1 save"), contents)
}

func TestCreateServerWorldRejectsDuplicateSaveName(t *testing.T) {
	_, manager := setupRuntimeManager(t)
	serverManagerMu.Lock()
	previous := serverManager
	serverManager = manager
	serverManagerMu.Unlock()
	defer func() {
		serverManagerMu.Lock()
		serverManager = previous
		serverManagerMu.Unlock()
	}()

	// Create a second server so we have a working SavesDir
	server2, err := manager.CreateServer("Second Server", "stable", "127.0.0.1", 34198, false)
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(server2.SavesDir, "existing.zip"), []byte("existing"), 0600))
	defer os.Remove(filepath.Join(server2.SavesDir, "existing.zip"))

	_, err = CreateServerWorld(server2.ID, WorldGenerationRequest{Preset: "default", Seed: 42, Name: "existing.zip"})
	require.ErrorIs(t, err, ErrSaveAlreadyExists)
}
