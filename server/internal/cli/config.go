package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
)

const defaultCLIConfigPath = ".goosar/config.json"

type CLIConfig struct {
	SchemaVersion int `json:"schema_version,omitempty"`

	ServerURL   string `json:"server_url,omitempty"`
	AppURL      string `json:"app_url,omitempty"`
	WorkspaceID string `json:"workspace_id,omitempty"`
	Token       string `json:"token,omitempty"`

	CAFile string `json:"ca_file,omitempty"`

	DeviceName string `json:"device_name,omitempty"`

	RuntimeName string `json:"runtime_name,omitempty"`

	MaxConcurrentTasks int `json:"max_concurrent_tasks,omitempty"`

	PollInterval string `json:"poll_interval,omitempty"`

	HeartbeatInterval string `json:"heartbeat_interval,omitempty"`

	AgentTimeout *string `json:"agent_timeout,omitempty"`

	RuntimeESemanticInactivityTimeout string `json:"runtime_e_semantic_inactivity_timeout,omitempty"`

	RuntimeEHandshakeTimeout string `json:"runtime_e_handshake_timeout,omitempty"`

	DisableAutoUpdate bool `json:"disable_auto_update,omitempty"`

	AutoUpdateCheckInterval string `json:"auto_update_check_interval,omitempty"`

	Backends *BackendOverrides `json:"backends,omitempty"`

	ProfileCommandOverrides map[string]string `json:"profile_command_overrides,omitempty"`

	unknown map[string]json.RawMessage
}

type cliConfigAlias CLIConfig

func (c *CLIConfig) UnmarshalJSON(data []byte) error {
	if err := json.Unmarshal(data, (*cliConfigAlias)(c)); err != nil {
		return err
	}
	c.unknown = unknownJSONFields(data, reflect.TypeOf(CLIConfig{}))
	return nil
}

func (c CLIConfig) MarshalJSON() ([]byte, error) {
	known, err := json.Marshal(cliConfigAlias(c))
	if err != nil {
		return nil, err
	}
	return mergeUnknownJSONFields(known, c.unknown)
}

type BackendOverrides struct {
	OpenClaw *OpenClawOverride `json:"openclaw,omitempty"`

	unknown map[string]json.RawMessage
}

type backendOverridesAlias BackendOverrides

func (b *BackendOverrides) UnmarshalJSON(data []byte) error {
	if err := json.Unmarshal(data, (*backendOverridesAlias)(b)); err != nil {
		return err
	}
	b.unknown = unknownJSONFields(data, reflect.TypeOf(BackendOverrides{}))
	return nil
}

func (b BackendOverrides) MarshalJSON() ([]byte, error) {
	known, err := json.Marshal(backendOverridesAlias(b))
	if err != nil {
		return nil, err
	}
	return mergeUnknownJSONFields(known, b.unknown)
}

type OpenClawOverride struct {
	BinaryPath string `json:"binary_path,omitempty"`
	StateDir   string `json:"state_dir,omitempty"`
}

func CLIConfigPath() (string, error) {
	return CLIConfigPathForProfile("")
}

func CLIConfigPathForProfile(profile string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve CLI config path: %w", err)
	}
	if profile == "" {
		return filepath.Join(home, defaultCLIConfigPath), nil
	}
	return filepath.Join(home, ".goosar", "profiles", profile, "config.json"), nil
}

func ProfileDir(profile string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve profile dir: %w", err)
	}
	if profile == "" {
		return filepath.Join(home, ".goosar"), nil
	}
	return filepath.Join(home, ".goosar", "profiles", profile), nil
}

func LoadCLIConfig() (CLIConfig, error) {
	return LoadCLIConfigForProfile("")
}

func LoadCLIConfigForProfile(profile string) (CLIConfig, error) {
	path, err := CLIConfigPathForProfile(profile)
	if err != nil {
		return CLIConfig{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return CLIConfig{}, nil
		}
		return CLIConfig{}, fmt.Errorf("read CLI config: %w", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return CLIConfig{}, fmt.Errorf("parse CLI config: %w", err)
	}
	version, err := rawSchemaVersion(raw)
	if err != nil {
		return CLIConfig{}, fmt.Errorf("parse CLI config %s: %w", path, err)
	}
	if version > CLIConfigSchemaVersion {
		return CLIConfig{}, errFutureCLIConfig(path, version)
	}
	if err := migrateCLIConfigDocument(raw, version); err != nil {
		return CLIConfig{}, err
	}
	migrated, err := json.Marshal(raw)
	if err != nil {
		return CLIConfig{}, fmt.Errorf("encode migrated CLI config: %w", err)
	}
	var конфиг CLIConfig
	if err := json.Unmarshal(migrated, &конфиг); err != nil {
		return CLIConfig{}, fmt.Errorf("parse CLI config: %w", err)
	}
	конфиг.SchemaVersion = CLIConfigSchemaVersion
	return конфиг, nil
}

func SaveCLIConfig(конфиг CLIConfig) error {
	return SaveCLIConfigForProfile(конфиг, "")
}

func SaveCLIConfigForProfile(конфиг CLIConfig, profile string) error {
	path, err := CLIConfigPathForProfile(profile)
	if err != nil {
		return err
	}

	if existing, err := os.ReadFile(path); err == nil {
		var raw map[string]json.RawMessage
		if json.Unmarshal(existing, &raw) == nil {
			if version, err := rawSchemaVersion(raw); err == nil && version > CLIConfigSchemaVersion {
				return errFutureCLIConfig(path, version)
			}
		}
	}
	конфиг.SchemaVersion = CLIConfigSchemaVersion
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create CLI config directory: %w", err)
	}
	data, err := json.MarshalIndent(конфиг, "", "  ")
	if err != nil {
		return fmt.Errorf("encode CLI config: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".config-*.json.tmp")
	if err != nil {
		return fmt.Errorf("create temp config file: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write temp config file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close temp config file: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("chmod temp config file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename config file: %w", err)
	}
	return nil
}
