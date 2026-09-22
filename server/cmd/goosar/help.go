package main

import (
	"fmt"
	"strings"
	"text/template"

	"github.com/spf13/cobra"
)

const (
	groupCore       = "core"
	groupRuntime    = "runtime"
	groupAdditional = "additional"
)

var errSilent = fmt.Errorf("")

func exactArgs(n int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) != n {
			if n == 1 {
				fmt.Fprintf(cmd.ErrOrStderr(), "Error: accepts 1 arg, received %d\n\n", len(args))
			} else {
				fmt.Fprintf(cmd.ErrOrStderr(), "Error: accepts %d args, received %d\n\n", n, len(args))
			}
			cmd.Help()
			return errSilent
		}
		return nil
	}
}

func initHelp(root *cobra.Command) {
	root.SetHelpTemplate(rootHelpTemplate)
	root.SetUsageTemplate(rootHelpTemplate)
	root.CompletionOptions.HiddenDefaultCmd = true

	root.AddGroup(
		&cobra.Group{ID: groupCore, Title: "ОСНОВНЫЕ КОМАНДЫ"},
		&cobra.Group{ID: groupRuntime, Title: "КОМАНДЫ СРЕДЫ ВЫПОЛНЕНИЯ"},
		&cobra.Group{ID: groupAdditional, Title: "ДОПОЛНИТЕЛЬНЫЕ КОМАНДЫ"},
	)

	applyTemplates(root)
}

func applyTemplates(cmd *cobra.Command) {
	for _, c := range cmd.Commands() {
		if c.HasSubCommands() {
			c.SetHelpTemplate(subHelpTemplate)
			c.SetUsageTemplate(subHelpTemplate)
		} else {
			c.SetHelpTemplate(leafHelpTemplate)
			c.SetUsageTemplate(leafHelpTemplate)
		}
		applyTemplates(c)
	}
}

func formatCommandList(cmds []*cobra.Command) string {
	if len(cmds) == 0 {
		return ""
	}

	maxLen := 0
	for _, c := range cmds {
		if c.IsAvailableCommand() && len(c.Name()) > maxLen {
			maxLen = len(c.Name())
		}
	}

	var b strings.Builder
	for _, c := range cmds {
		if !c.IsAvailableCommand() {
			continue
		}
		padding := strings.Repeat(" ", maxLen-len(c.Name()))
		fmt.Fprintf(&b, "  %s:%s  %s\n", c.Name(), padding, c.Short)
	}
	return b.String()
}

func commandsInGroup(cmds []*cobra.Command, groupID string) []*cobra.Command {
	var result []*cobra.Command
	for _, c := range cmds {
		if c.GroupID == groupID && c.IsAvailableCommand() {
			result = append(result, c)
		}
	}
	return result
}

func init() {
	cobra.AddTemplateFuncs(template.FuncMap{
		"formatCommandList": formatCommandList,
		"commandsInGroup":   commandsInGroup,
	})
}

var rootHelpTemplate = `Работайте с Goosar из командной строки.

ИСПОЛЬЗОВАНИЕ
  goosar <command> <subcommand> [flags]
{{range .Groups}}
{{.Title}}
{{formatCommandList (commandsInGroup $.Commands .ID)}}
{{- end}}
ФЛАГИ
{{.LocalFlags.FlagUsages}}
ПРИМЕРЫ
  $ goosar login
  $ goosar issue list --output json
  $ goosar daemon start
  $ goosar agent list --output json

ПЕРЕМЕННЫЕ ОКРУЖЕНИЯ
  GOOSAR_SERVER_URL    Переопределить URL сервера по умолчанию
  GOOSAR_WORKSPACE_ID  Задать активное рабочее пространство

ПОДРОБНЕЕ
  Справка по конкретной команде: ` + "`goosar <command> <subcommand> --help`" + `.
`

var subHelpTemplate = `{{.Short}}

ИСПОЛЬЗОВАНИЕ
  {{.CommandPath}} <command> [flags]

КОМАНДЫ
{{formatCommandList .Commands}}
{{- if .HasLocalFlags}}

ФЛАГИ
{{.LocalFlags.FlagUsages}}
{{- end}}
УНАСЛЕДОВАННЫЕ ФЛАГИ
  --help   Показать справку по команде
{{- if .Example}}

ПРИМЕРЫ
{{.Example}}
{{- end}}

ПОДРОБНЕЕ
  Справка по конкретной команде: ` + "`{{.CommandPath}} <command> --help`" + `.
`

var leafHelpTemplate = `{{if .Long}}{{.Long}}{{else}}{{.Short}}{{end}}

ИСПОЛЬЗОВАНИЕ
  {{.UseLine}}
{{- if .HasLocalFlags}}

ФЛАГИ
{{.LocalFlags.FlagUsages}}
{{- end}}
УНАСЛЕДОВАННЫЕ ФЛАГИ
  --help   Показать справку по команде
{{- if .Example}}

ПРИМЕРЫ
{{.Example}}
{{- end}}

ПОДРОБНЕЕ
  Справка по конкретной команде: ` + "`goosar <command> <subcommand> --help`" + `.
`
