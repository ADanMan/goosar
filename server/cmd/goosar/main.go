package main

import (
	"fmt"
	"log/slog"
	"os"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/adanman/goosar/server/internal/cli"
	"github.com/adanman/goosar/server/internal/daemon/execenv"
)

var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

var debugFlag bool

var rootCmd = &cobra.Command{
	Use:           "goosar",
	Short:         "Goosar CLI — локальная среда выполнения агентов и инструмент управления",
	Long:          "Работайте с Goosar из командной строки.",
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	rootCmd.Version = fmt.Sprintf("%s (commit: %s, built: %s)\ngo: %s, os/arch: %s/%s", version, commit, date, runtime.Version(), runtime.GOOS, runtime.GOARCH)
	rootCmd.SetVersionTemplate("goosar {{.Version}}\n")

	cli.ClientVersion = version

	rootCmd.PersistentFlags().String("server-url", "", "URL сервера Goosar (env: GOOSAR_SERVER_URL)")
	rootCmd.PersistentFlags().String("workspace-id", "", "ID рабочего пространства (env: GOOSAR_WORKSPACE_ID)")
	rootCmd.PersistentFlags().String("profile", "", "Имя профиля конфигурации (например, dev) — отдельные конфигурация, состояние демона и рабочие пространства")
	rootCmd.PersistentFlags().BoolVar(&debugFlag, "debug", false, "Показывать полные сведения об ошибке (env: GOOSAR_DEBUG)")
	rootCmd.PersistentFlags().String("ca-file", "", "PEM-набор сертификатов, которым доверять помимо системных CA, например stand-root-ca.crt стенда (env: GOOSAR_CA_FILE; сохраняется командой `goosar setup self-host`)")

	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error {
		flagValue, _ := cmd.Flags().GetString("ca-file")
		var cfg cli.CLIConfig
		if loaded, err := cli.LoadCLIConfigForProfile(resolveProfile(cmd)); err == nil {
			cfg = loaded
		}
		path := cli.ResolveCAFile(flagValue, cfg)
		if err := cli.ConfigureCA(path); err != nil {
			return err
		}
		if path != "" {
			cli.ExportCAEnv(path)
		}
		return nil
	}

	issueCmd.GroupID = groupCore
	projectCmd.GroupID = groupCore
	labelCmd.GroupID = groupCore
	propertyCmd.GroupID = groupCore
	agentCmd.GroupID = groupCore
	autopilotCmd.GroupID = groupCore
	workspaceCmd.GroupID = groupCore
	repoCmd.GroupID = groupCore
	skillCmd.GroupID = groupCore
	squadCmd.GroupID = groupCore
	chatCmd.GroupID = groupCore

	daemonCmd.GroupID = groupRuntime
	runtimeCmd.GroupID = groupRuntime

	authCmd.GroupID = groupAdditional
	userCmd.GroupID = groupAdditional
	loginCmd.GroupID = groupAdditional
	setupCmd.GroupID = groupAdditional
	attachmentCmd.GroupID = groupAdditional
	configCmd.GroupID = groupAdditional
	updateCmd.GroupID = groupAdditional
	versionCmd.GroupID = groupAdditional
	statusCmd.GroupID = groupAdditional
	doctorCmd.GroupID = groupAdditional

	rootCmd.AddCommand(issueCmd)
	rootCmd.AddCommand(projectCmd)
	rootCmd.AddCommand(labelCmd)
	rootCmd.AddCommand(propertyCmd)
	rootCmd.AddCommand(agentCmd)
	rootCmd.AddCommand(autopilotCmd)
	rootCmd.AddCommand(workspaceCmd)
	rootCmd.AddCommand(repoCmd)
	rootCmd.AddCommand(skillCmd)
	rootCmd.AddCommand(squadCmd)
	rootCmd.AddCommand(chatCmd)
	rootCmd.AddCommand(daemonCmd)
	rootCmd.AddCommand(runtimeCmd)
	rootCmd.AddCommand(authCmd)
	rootCmd.AddCommand(userCmd)
	rootCmd.AddCommand(loginCmd)
	rootCmd.AddCommand(setupCmd)
	rootCmd.AddCommand(attachmentCmd)
	rootCmd.AddCommand(configCmd)
	rootCmd.AddCommand(updateCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(doctorCmd)
	rootCmd.AddCommand(supportBundleCmd)

	initHelp(rootCmd)
}

func main() {
	if len(os.Args) == 2 && os.Args[1] == execenv.PreparationHelperArg {
		logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
		if err := execenv.RunPreparationHelper(os.Stdin, os.Stdout, logger); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	cli.CleanupStaleUpdateArtifacts()
	if err := rootCmd.Execute(); err != nil {
		if err != errSilent {
			fmt.Fprintln(os.Stderr, cli.FormatError(err, debugFlag))
		}
		os.Exit(cli.ExitCodeFor(err))
	}
}
