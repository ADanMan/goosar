package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/adanman/goosar/server/internal/cli"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Управление настройками goosar",
	RunE:  runConfigShow,
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Показать текущую конфигурацию CLI",
	RunE:  runConfigShow,
}

var configSetSupportedKeys = []string{
	"server_url",
	"app_url",
	"workspace_id",
	"device_name",
	"runtime_name",
	"max_concurrent_tasks",
	"poll_interval",
	"heartbeat_interval",
	"agent_timeout",
	"runtime_e_semantic_inactivity_timeout",
	"runtime_e_handshake_timeout",
	"disable_auto_update",
	"auto_update_check_interval",
}

var configSetCmd = &cobra.Command{
	Use:   "set <key> <value>",
	Short: "Задать значение конфигурации CLI",
	Long: "Поддерживаемые ключи: " +
		"server_url, app_url, workspace_id, " +
		"device_name, runtime_name, max_concurrent_tasks, poll_interval, " +
		"heartbeat_interval, agent_timeout, " +
		"runtime_e_semantic_inactivity_timeout, runtime_e_handshake_timeout, " +
		"disable_auto_update, auto_update_check_interval.\n\n" +
		"Ключи демона (device_name, runtime_name, max_concurrent_tasks, " +
		"poll_interval, heartbeat_interval, agent_timeout, " +
		"runtime_e_semantic_inactivity_timeout, runtime_e_handshake_timeout, " +
		"disable_auto_update, auto_update_check_interval) соответствуют своим " +
		"флагам и переменным окружения; `daemon start` читает их, когда " +
		"не задан ни флаг, ни переменная окружения. " +
		"Приоритет: --flag > переменная окружения GOOSAR_… > config.json > значение по умолчанию. " +
		"Ключи с длительностью принимают положительную длительность Go (например, '10s', '500ms', '1m30s'); " +
		"'0s' и отрицательные значения отклоняются — кроме agent_timeout, где " +
		"'0s' имеет смысл и явно отключает ограничение по времени выполнения. " +
		"disable_auto_update принимает 'true' или 'false' (работает в одну сторону: " +
		"'true' отключает автообновление, 'false' сбрасывает переопределение, " +
		"и решают переменная окружения или значение по умолчанию). Пустая строка " +
		"очищает сохранённое значение (например, `config set poll_interval \"\"`).",
	Args: exactArgs(2),
	RunE: runConfigSet,
}

func init() {
	configCmd.AddCommand(configShowCmd)
	configCmd.AddCommand(configSetCmd)
}

func runConfigShow(cmd *cobra.Command, _ []string) error {
	профиль := resolveProfile(cmd)
	cfg, err := cli.LoadCLIConfigForProfile(профиль)
	if err != nil {
		return err
	}

	path, _ := cli.CLIConfigPathForProfile(профиль)
	fmt.Fprintf(os.Stdout, "Config file: %s\n", path)
	if профиль != "" {
		fmt.Fprintf(os.Stdout, "Profile:      %s\n", профиль)
	}
	fmt.Fprintf(os.Stdout, "%-34s %s\n", "server_url:", valueOrDefault(cfg.ServerURL, "(not set)"))
	fmt.Fprintf(os.Stdout, "%-34s %s\n", "app_url:", valueOrDefault(cfg.AppURL, "(not set)"))
	fmt.Fprintf(os.Stdout, "%-34s %s\n", "workspace_id:", valueOrDefault(cfg.WorkspaceID, "(not set)"))
	fmt.Fprintf(os.Stdout, "%-34s %s\n", "device_name:", valueOrDefault(cfg.DeviceName, "(not set)"))
	fmt.Fprintf(os.Stdout, "%-34s %s\n", "runtime_name:", valueOrDefault(cfg.RuntimeName, "(not set)"))
	fmt.Fprintf(os.Stdout, "%-34s %s\n", "max_concurrent_tasks:", intOrDefault(cfg.MaxConcurrentTasks, "(not set)"))
	fmt.Fprintf(os.Stdout, "%-34s %s\n", "poll_interval:", valueOrDefault(cfg.PollInterval, "(not set)"))
	fmt.Fprintf(os.Stdout, "%-34s %s\n", "heartbeat_interval:", valueOrDefault(cfg.HeartbeatInterval, "(not set)"))
	fmt.Fprintf(os.Stdout, "%-34s %s\n", "agent_timeout:", agentTimeoutDisplay(cfg.AgentTimeout))
	fmt.Fprintf(os.Stdout, "%-34s %s\n", "runtime_e_semantic_inactivity_timeout:", valueOrDefault(cfg.RuntimeESemanticInactivityTimeout, "(not set)"))
	fmt.Fprintf(os.Stdout, "%-34s %s\n", "runtime_e_handshake_timeout:", valueOrDefault(cfg.RuntimeEHandshakeTimeout, "(not set)"))
	fmt.Fprintf(os.Stdout, "%-34s %t\n", "disable_auto_update:", cfg.DisableAutoUpdate)
	fmt.Fprintf(os.Stdout, "%-34s %s\n", "auto_update_check_interval:", valueOrDefault(cfg.AutoUpdateCheckInterval, "(not set)"))
	return nil
}

func runConfigSet(cmd *cobra.Command, args []string) error {
	key, value := args[0], args[1]

	профиль := resolveProfile(cmd)
	cfg, err := cli.LoadCLIConfigForProfile(профиль)
	if err != nil {
		return err
	}

	if err := applyConfigSet(&cfg, key, value); err != nil {
		return err
	}

	if err := cli.SaveCLIConfigForProfile(cfg, профиль); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "Set %s = %s\n", key, value)
	return nil
}

func applyConfigSet(cfg *cli.CLIConfig, key, value string) error {
	switch key {
	case "server_url":
		cfg.ServerURL = value
	case "app_url":
		cfg.AppURL = value
	case "workspace_id":
		cfg.WorkspaceID = value
	case "device_name":
		cfg.DeviceName = value
	case "runtime_name":
		cfg.RuntimeName = value
	case "max_concurrent_tasks":
		if value == "" {
			cfg.MaxConcurrentTasks = 0
			return nil
		}
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("max_concurrent_tasks must be an integer: %w", err)
		}
		if n < 0 {
			return fmt.Errorf("max_concurrent_tasks must be >= 0 (got %d)", n)
		}
		cfg.MaxConcurrentTasks = n
	case "poll_interval":
		if value == "" {
			cfg.PollInterval = ""
			return nil
		}
		d, err := time.ParseDuration(value)
		if err != nil {
			return fmt.Errorf("poll_interval must be a Go duration (e.g. 10s, 500ms): %w", err)
		}

		if d <= 0 {
			return fmt.Errorf("poll_interval must be positive (got %s); use `config set poll_interval \"\"` to clear it", d)
		}
		cfg.PollInterval = value
	case "heartbeat_interval":
		if err := assignPositiveDuration(&cfg.HeartbeatInterval, key, value); err != nil {
			return err
		}
	case "agent_timeout":

		if value == "" {
			cfg.AgentTimeout = nil
			return nil
		}
		d, err := time.ParseDuration(value)
		if err != nil {
			return fmt.Errorf("agent_timeout must be a Go duration (e.g. 10m, 0s to disable): %w", err)
		}
		if d < 0 {
			return fmt.Errorf("agent_timeout must be >= 0 (got %s); use 0s to disable the cap or \"\" to clear the persisted value", d)
		}
		s := value
		cfg.AgentTimeout = &s
	case "runtime_e_semantic_inactivity_timeout":
		if err := assignPositiveDuration(&cfg.RuntimeESemanticInactivityTimeout, key, value); err != nil {
			return err
		}
	case "runtime_e_handshake_timeout":
		if err := assignPositiveDuration(&cfg.RuntimeEHandshakeTimeout, key, value); err != nil {
			return err
		}
	case "disable_auto_update":
		if value == "" {
			cfg.DisableAutoUpdate = false
			return nil
		}
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("disable_auto_update must be 'true' or 'false' (got %q)", value)
		}
		cfg.DisableAutoUpdate = b
	case "auto_update_check_interval":
		if err := assignPositiveDuration(&cfg.AutoUpdateCheckInterval, key, value); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown config key %q (supported: %s)", key, joinKeys(configSetSupportedKeys))
	}
	return nil
}

func assignPositiveDuration(dst *string, key, value string) error {
	if value == "" {
		*dst = ""
		return nil
	}
	normalized := strings.TrimSpace(value)
	d, err := time.ParseDuration(normalized)
	if err != nil {
		return fmt.Errorf("%s must be a Go duration (e.g. 10s, 500ms): %w", key, err)
	}
	if d <= 0 {
		return fmt.Errorf("%s must be positive (got %s); use `config set %s \"\"` to clear it", key, d, key)
	}
	*dst = normalized
	return nil
}

func agentTimeoutDisplay(v *string) string {
	if v == nil {
		return "(not set)"
	}
	if *v == "" {
		return "(not set)"
	}
	if d, err := time.ParseDuration(*v); err == nil && d == 0 {
		return *v + " (disabled)"
	}
	return *v
}

func valueOrDefault(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

func intOrDefault(v int, fallback string) string {
	if v == 0 {
		return fallback
	}
	return strconv.Itoa(v)
}

func joinKeys(keys []string) string {
	out := ""
	for i, k := range keys {
		if i > 0 {
			out += ", "
		}
		out += k
	}
	return out
}
