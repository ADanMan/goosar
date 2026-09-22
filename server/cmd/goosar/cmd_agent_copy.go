package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/adanman/goosar/server/internal/cli"
)

var agentCopyCmd = &cobra.Command{
	Use:   "copy <source-agent-id>",
	Short: "Скопировать агента в нового (при желании — на другую среду выполнения)",
	Long: `Копирует переносимую конфигурацию существующего агента в нового агента.

Исходный агент не меняется. По умолчанию копия создаётся на той же среде
выполнения, что и исходный агент; чтобы перенести её на другую среду
выполнения, передайте --runtime-id.

По умолчанию копируются следующие поля, каждое можно переопределить своим
флагом: name (с суффиксом " (copy)"), description, instructions, avatar,
custom_args, max_concurrent_tasks, права на вызов (permission_mode и список
разрешённых), назначенные skills рабочего пространства и — только если целевая
среда выполнения не меняется — model, thinking_level и service_tier.

Секретные и локальные для машины поля не копируются никогда: custom_env,
mcp_config и runtime_config. Если они нужны на целевой машине, задайте для
копии новые значения теми же безопасными для секретов флагами, что и в
'agent create'.

Поля, привязанные к среде выполнения, при её смене не переносятся: если
--runtime-id указывает на другую среду выполнения, --model обязателен
(передайте --model "", чтобы взять значение по умолчанию целевой среды), а
thinking_level и service_tier сбрасываются, пока вы не зададите их явно.`,
	Args: exactArgs(1),
	RunE: runAgentCopy,
}

func init() {
	agentCmd.AddCommand(agentCopyCmd)
	registerAgentCopyFlags(agentCopyCmd)
}

func registerAgentCopyFlags(cmd *cobra.Command) {
	cmd.Flags().String("name", "", "Имя нового агента (по умолчанию: \"<имя исходного> (copy)\")")
	cmd.Flags().String("runtime-id", "", "ID целевой среды выполнения (по умолчанию — среда исходного агента). Другое значение переносит агента на эту среду выполнения.")
	cmd.Flags().String("description", "", "Заменить скопированное описание")
	cmd.Flags().String("instructions", "", "Заменить скопированные инструкции")
	cmd.Flags().String("model", "", "Идентификатор модели для копии. Обязателен, если --runtime-id указывает на другую среду выполнения (передайте \"\", чтобы взять значение по умолчанию целевой среды). В остальных случаях пустое значение означает значение по умолчанию среды выполнения.")
	cmd.Flags().String("thinking-level", "", "Заменить уровень рассуждений. При смене среды выполнения не переносится, пока не задан здесь.")
	cmd.Flags().String("service-tier", "", "Заменить сервисный уровень выполнения среды. При смене среды выполнения не переносится, пока не задан здесь.")
	cmd.Flags().String("custom-args", "", "Заменить пользовательские аргументы CLI в виде JSON-массива.")
	cmd.Flags().Int32("max-concurrent-tasks", 6, "Заменить максимум одновременных задач")
	cmd.Flags().String("visibility", "", "Заменить видимость: private или workspace (сопоставляется с --permission-mode)")
	cmd.Flags().String("permission-mode", "", "Заменить режим прав на вызов: private или public_to. Приоритетнее --visibility.")
	cmd.Flags().Bool("public-to-workspace", false, "public_to: разрешить вызывать копию всем участникам рабочего пространства.")
	cmd.Flags().StringSlice("public-to-member", nil, "public_to: разрешить вызывать копию указанным участникам (user id). Можно повторять.")
	cmd.Flags().Bool("no-skills", false, "Не копировать назначенные исходному агенту skills рабочего пространства.")

	cmd.Flags().String("custom-env", "", "Задать custom_env копии как JSON-объект (из исходного агента не копируется). Для секретов лучше --custom-env-stdin/--custom-env-file. Для пустой карты передайте '{}'.")
	cmd.Flags().Bool("custom-env-stdin", false, "Прочитать --custom-env из stdin. Несовместим с --custom-env и --custom-env-file.")
	cmd.Flags().String("custom-env-file", "", "Прочитать --custom-env из файла по указанному пути (рекомендуемые права: 0600). Несовместим с --custom-env и --custom-env-stdin.")
	cmd.Flags().String("mcp-config", "", "Задать mcp_config копии как JSON-объект (из исходного агента не копируется). Для секретов лучше --mcp-config-stdin/--mcp-config-file.")
	cmd.Flags().Bool("mcp-config-stdin", false, "Прочитать --mcp-config из stdin. Несовместим с --mcp-config и --mcp-config-file.")
	cmd.Flags().String("mcp-config-file", "", "Прочитать --mcp-config из файла по указанному пути (рекомендуемые права: 0600). Несовместим с --mcp-config и --mcp-config-stdin.")
	cmd.Flags().String("runtime-config", "", "Задать runtime_config копии как JSON-строку (из исходного агента не копируется).")
	cmd.Flags().String("output", "json", "Формат вывода: table или json")
}

func runAgentCopy(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var src map[string]any
	if err := client.GetJSON(ctx, "/api/agents/"+args[0], &src); err != nil {
		return fmt.Errorf("get source agent: %w", err)
	}

	srcRuntimeID := strVal(src, "runtime_id")

	targetRuntimeID := srcRuntimeID
	if cmd.Flags().Changed("runtime-id") {
		v, _ := cmd.Flags().GetString("runtime-id")
		if v == "" {
			return fmt.Errorf("--runtime-id must not be empty")
		}
		targetRuntimeID = v
	}
	if targetRuntimeID == "" {
		return fmt.Errorf("source agent has no runtime; pass --runtime-id to choose a target runtime")
	}
	sameRuntime := targetRuntimeID == srcRuntimeID

	name := strVal(src, "name") + " (copy)"
	if cmd.Flags().Changed("name") {
		v, _ := cmd.Flags().GetString("name")
		if v == "" {
			return fmt.Errorf("--name must not be empty")
		}
		name = v
	}

	body := map[string]any{
		"name":       name,
		"runtime_id": targetRuntimeID,
	}

	body["description"] = strVal(src, "description")
	if cmd.Flags().Changed("description") {
		v, _ := cmd.Flags().GetString("description")
		body["description"] = v
	}
	body["instructions"] = strVal(src, "instructions")
	if cmd.Flags().Changed("instructions") {
		v, _ := cmd.Flags().GetString("instructions")
		body["instructions"] = v
	}

	if av, ok := src["avatar_url"]; ok && av != nil {
		body["avatar_url"] = av
	}

	if ca, ok := src["custom_args"].([]any); ok && len(ca) > 0 {
		body["custom_args"] = ca
	}
	if cmd.Flags().Changed("custom-args") {
		v, _ := cmd.Flags().GetString("custom-args")
		ca, err := parseCustomArgs(v)
		if err != nil {
			return err
		}
		body["custom_args"] = ca
	}

	if v, ok := src["max_concurrent_tasks"]; ok && v != nil {
		body["max_concurrent_tasks"] = v
	}
	if cmd.Flags().Changed("max-concurrent-tasks") {
		v, _ := cmd.Flags().GetInt32("max-concurrent-tasks")
		body["max_concurrent_tasks"] = v
	}

	if sameRuntime {
		if v := strVal(src, "model"); v != "" {
			body["model"] = v
		}
		if v := strVal(src, "thinking_level"); v != "" {
			body["thinking_level"] = v
		}
		if v := strVal(src, "service_tier"); v != "" {
			body["service_tier"] = v
		}
	} else if !cmd.Flags().Changed("model") {
		return fmt.Errorf("copying to a different runtime (--runtime-id) requires --model, because the source model may not exist on the target runtime; pass --model \"\" to accept the target runtime default")
	}
	if cmd.Flags().Changed("model") {
		v, _ := cmd.Flags().GetString("model")
		body["model"] = v
	}
	if cmd.Flags().Changed("thinking-level") {
		v, _ := cmd.Flags().GetString("thinking-level")
		body["thinking_level"] = v
	}
	if cmd.Flags().Changed("service-tier") {
		v, _ := cmd.Flags().GetString("service-tier")
		body["service_tier"] = v
	}

	permOverride := cmd.Flags().Changed("permission-mode") ||
		cmd.Flags().Changed("public-to-workspace") ||
		cmd.Flags().Changed("public-to-member") ||
		cmd.Flags().Changed("visibility")
	if permOverride {
		if cmd.Flags().Changed("visibility") {
			v, _ := cmd.Flags().GetString("visibility")
			body["visibility"] = v
		}
		applyAgentPermissionFlags(cmd, body)
	} else {
		if pm := strVal(src, "permission_mode"); pm != "" {
			body["permission_mode"] = pm
		}
		if it, ok := src["invocation_targets"]; ok && it != nil {
			body["invocation_targets"] = it
		}
	}

	if noSkills, _ := cmd.Flags().GetBool("no-skills"); !noSkills {
		if skills, ok := src["skills"].([]any); ok {
			ids := make([]string, 0, len(skills))
			for _, s := range skills {
				m, ok := s.(map[string]any)
				if !ok {
					continue
				}
				if id := strVal(m, "id"); id != "" {
					ids = append(ids, id)
				}
			}
			if len(ids) > 0 {
				body["skill_ids"] = ids
			}
		}
	}

	if ce, ok, err := resolveCustomEnv(cmd); err != nil {
		return err
	} else if ok {
		body["custom_env"] = ce
	}
	if mc, ok, err := resolveMcpConfig(cmd); err != nil {
		return err
	} else if ok {
		body["mcp_config"] = mc
	}
	if cmd.Flags().Changed("runtime-config") {
		v, _ := cmd.Flags().GetString("runtime-config")
		var rc any
		if err := json.Unmarshal([]byte(v), &rc); err != nil {
			return fmt.Errorf("--runtime-config must be valid JSON: %w", err)
		}
		body["runtime_config"] = rc
	}

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/agents", body, &result); err != nil {
		return fmt.Errorf("copy agent: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}

	fmt.Printf("Agent copied: %s (%s)\n", strVal(result, "name"), strVal(result, "id"))
	return nil
}
