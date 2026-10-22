package factorio

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/OpenFactorioServerManager/factorio-server-manager/bootstrap"
	"gopkg.in/ini.v1"
)

func validateManagedVersionTarget(version Version, target string) error {
	if version == NilVersion {
		return fmt.Errorf("%w: installed Factorio version is unavailable", ErrInvalidServerConfiguration)
	}
	if target == "" || target == "stable" || target == "latest" {
		return nil
	}
	exact, err := NormalizeExactReleaseVersion(target)
	if err != nil {
		return fmt.Errorf("%w: invalid exact version target", ErrInvalidServerConfiguration)
	}
	if exact != version.ReleaseString() {
		return fmt.Errorf("%w: requested %s differs from shared installed %s; independent installations are not supported", ErrInvalidServerConfiguration, exact, version.ReleaseString())
	}
	return nil
}

func managedRuntimeINI(data []byte, readData, writeData string) ([]byte, error) {
	if strings.IndexFunc(readData+writeData, func(character rune) bool { return character < 32 || character == 127 }) >= 0 {
		return nil, fmt.Errorf("%w: runtime paths contain control characters", ErrInvalidServerConfiguration)
	}
	var output []string
	inPath, seenPath := false, false
	for _, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			if end := strings.Index(trimmed, "]"); end > 0 {
				inPath = strings.EqualFold(strings.TrimSpace(trimmed[1:end]), "path")
				if inPath {
					output = append(output, "[path]", "read-data="+readData, "write-data="+writeData)
					seenPath = true
					if comment := strings.TrimSpace(trimmed[end+1:]); comment != "" {
						output = append(output, "; "+comment)
					}
					continue
				}
			}
		}
		key, _, hasValue := strings.Cut(line, "=")
		if inPath && hasValue && (strings.EqualFold(strings.TrimSpace(key), "read-data") || strings.EqualFold(strings.TrimSpace(key), "write-data")) {
			continue
		}
		output = append(output, line)
	}
	if !seenPath {
		output = append(output, "[path]", "read-data="+readData, "write-data="+writeData)
	}
	text := strings.Join(output, "\n")
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return []byte(text), nil
}

// The current launcher uses one installed executable. Only its version metadata
// is shared; a managed runtime never inherits the default game's private state.
func prepareManagedRuntime(server *Server, installed Server) (swaps []*directoryEntrySwap, err error) {
	defer func() {
		if err != nil {
			cleanupProfileSwaps(swaps)
		}
	}()
	root := filepath.Dir(server.ConfigDir)
	for _, directory := range []string{filepath.Dir(root), root, server.SavesDir, server.ModsDir, server.ConfigDir} {
		info, statErr := os.Lstat(directory)
		if errors.Is(statErr, os.ErrNotExist) {
			if err := os.Mkdir(directory, 0700); err != nil {
				return swaps, fmt.Errorf("create managed storage: %w", err)
			}
		} else if statErr != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return swaps, fmt.Errorf("%w: managed storage must use real directories", ErrInvalidServerConfiguration)
		}
	}
	defaults := map[string]interface{}{
		"name": server.Name, "description": "", "tags": []string{},
		"visibility":                map[string]bool{"public": false, "lan": true},
		"require_user_verification": true, "allow_commands": "admins-only",
	}
	var settings map[string]interface{}
	settingsMissing := false
	if err := readBackupJSON(server.SettingsFile(), &settings); errors.Is(err, os.ErrNotExist) {
		settingsMissing = true
		settings = defaults
	} else if err != nil || settings == nil {
		return swaps, fmt.Errorf("%w: managed server settings must be a valid object", ErrInvalidServerConfiguration)
	}
	admins := []string{}
	adminPath := filepath.Join(server.ConfigDir, "server-adminlist.json")
	adminsMissing := false
	if err := readBackupJSON(adminPath, &admins); errors.Is(err, os.ErrNotExist) {
		adminsMissing = true
		if embedded, ok := settings["admins"]; ok {
			data, marshalErr := json.Marshal(embedded)
			if marshalErr != nil || json.Unmarshal(data, &admins) != nil || admins == nil {
				return swaps, fmt.Errorf("%w: managed admin list is invalid", ErrInvalidServerConfiguration)
			}
		}
	} else if err != nil || admins == nil {
		return swaps, fmt.Errorf("%w: managed admin list must be an array", ErrInvalidServerConfiguration)
	}
	settings["admins"] = admins
	configMissing := false
	configPath := filepath.Join(server.ConfigDir, "config.ini")
	data, readErr := readBackupFile(configPath, backupMaxJSONBytes)
	if errors.Is(readErr, os.ErrNotExist) {
		configMissing = true
	} else if readErr != nil {
		return swaps, fmt.Errorf("%w: managed config.ini cannot be read", ErrInvalidServerConfiguration)
	} else {
		if _, err := ini.Load(data); err != nil {
			return swaps, fmt.Errorf("%w: managed config.ini is invalid", ErrInvalidServerConfiguration)
		}
	}
	dataPath, err := filepath.Abs(filepath.Join(bootstrap.GetConfig().FactorioDir, "data"))
	if err != nil {
		return swaps, err
	}
	readData, writeData := filepath.ToSlash(dataPath), filepath.ToSlash(root)
	nativeConfig, err := managedRuntimeINI(data, readData, writeData)
	if err != nil {
		return swaps, err
	}
	configChanged := configMissing || !bytes.Equal(data, nativeConfig)
	if settingsMissing || adminsMissing || configChanged {
		swap, err := newDirectoryEntrySwap(server.ConfigDir, ".runtime-staging-", ".runtime-backup-")
		if err != nil {
			return swaps, err
		}
		swaps = append(swaps, swap)
		excluded := map[string]bool{filepath.Base(swap.staging): true, filepath.Base(swap.backup): true}
		if err := copyPresetDirectory(server.ConfigDir, swap.staging, &backupCopyBudget{}, excluded); err != nil {
			return swaps, fmt.Errorf("stage managed configuration: %w", err)
		}
		if settingsMissing {
			if err := writeBackupJSON(filepath.Join(swap.staging, "server-settings.json"), settings); err != nil {
				return swaps, err
			}
		}
		if adminsMissing {
			if err := writeBackupJSON(filepath.Join(swap.staging, "server-adminlist.json"), admins); err != nil {
				return swaps, err
			}
		}
		if configChanged {
			if err := os.WriteFile(filepath.Join(swap.staging, "config.ini"), nativeConfig, 0600); err != nil {
				return swaps, err
			}
		}
	}
	var mods ModSimpleList
	if err := readBackupJSON(filepath.Join(server.ModsDir, "mod-list.json"), &mods); errors.Is(err, os.ErrNotExist) {
		entries, err := os.ReadDir(server.ModsDir)
		if err != nil || len(entries) != 0 {
			return swaps, fmt.Errorf("%w: existing managed mods require their own mod-list.json", ErrInvalidServerConfiguration)
		}
		swap, err := newDirectoryEntrySwap(server.ModsDir, ".runtime-staging-", ".runtime-backup-")
		if err != nil {
			return swaps, err
		}
		swaps = append(swaps, swap)
		mods.Mods = []ModSimple{{Name: "base", Enabled: true}}
		for _, name := range append(append([]string{}, spaceAgeFeatureMods...), presetAdditionalBuiltIns(installed.Version)...) {
			mods.Mods = append(mods.Mods, ModSimple{Name: name, Enabled: false})
		}
		if err := writeBackupJSON(filepath.Join(swap.staging, "mod-list.json"), mods); err != nil {
			return swaps, err
		}
	} else if err != nil || mods.Mods == nil {
		return swaps, fmt.Errorf("%w: managed mod-list.json is invalid", ErrInvalidServerConfiguration)
	}
	server.Settings, server.Version, server.BaseModVersion = settings, installed.Version, installed.BaseModVersion
	return swaps, nil
}

func finishManagedRuntimeSwaps(swaps []*directoryEntrySwap, result *error) {
	if *result != nil {
		for i := len(swaps) - 1; i >= 0; i-- {
			if swaps[i].isActive {
				*result = errors.Join(*result, swaps[i].rollback())
			}
		}
	} else {
		for _, swap := range swaps {
			if err := swap.commit(); err != nil {
				log.Printf("Managed runtime committed but transaction cleanup failed")
			}
		}
	}
	cleanupProfileSwaps(swaps)
}
