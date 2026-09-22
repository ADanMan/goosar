package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/adanman/goosar/server/internal/cli"
	"github.com/adanman/goosar/server/internal/preflight"
)

var doctorCmd = newDoctorCmd()

func newDoctorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Проверить эту машину: goosar в PATH, agent CLI, Node/npm, git, Kerberos, Docker, пакеты MCP; с --server — связь со стендом",
		Long: "Проверяет зависимости демона на этой машине и печатает, чего не хватает и что именно поставить.\n\n" +
			"Работает локально: обращений к серверу нет. Agent CLI не запускается — только проверяется наличие " +
			"исполняемого файла и его --version.",
		RunE:          runDoctor,
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	cmd.Flags().String("output", "table", "Формат вывода: table или json")
	cmd.Flags().Bool("kerberos", false, "Считать Kerberos (kinit) обязательным на этой машине")

	cmd.Flags().Bool("server", false, "Также проверить связь со стендом: конфигурация, сервер, сертификат, демон, кит для этой платформы")

	cmd.Flags().String("profile", "", "Профиль CLI (по умолчанию — основной)")
	return cmd
}

func runDoctor(cmd *cobra.Command, _ []string) error {
	requireKerberos, _ := cmd.Flags().GetBool("kerberos")
	self := &preflight.SelfInfo{Version: version}
	if exe, err := os.Executable(); err == nil {
		self.Path = exe
	}
	report := preflight.Run(context.Background(), preflight.Options{Self: self, RequireKerberos: requireKerberos})
	if withServer, _ := cmd.Flags().GetBool("server"); withServer {
		rows := preflight.RunServer(context.Background(), doctorServerOptions(resolveProfile(cmd)))
		report.Results = append(report.Results, rows...)
		report.OK = len(report.Failed()) == 0
	}

	format, _ := cmd.Flags().GetString("output")
	if format == "json" {
		if err := cli.PrintJSON(cmd.OutOrStdout(), report); err != nil {
			return err
		}
	} else {
		printDoctorTable(cmd.OutOrStdout(), report)
	}

	if !report.OK {

		return errSilent
	}
	return nil
}

func printDoctorTable(w io.Writer, report preflight.Report) {
	rows := make([][]string, 0, len(report.Results))
	for _, r := range report.Results {
		detail := r.Detail
		if detail == "" {
			detail = r.Message
		}
		rows = append(rows, []string{r.Name, doctorStatusLabel(r.Status), detail})
	}
	cli.PrintTable(w, []string{"ЗАВИСИМОСТЬ", "СОСТОЯНИЕ", "ЧТО НАЙДЕНО"}, rows)

	failed := report.Failed()
	if len(failed) == 0 {
		fmt.Fprintln(w, "\nВсё на месте: демон можно запускать (goosar daemon start).")
		return
	}
	fmt.Fprintf(w, "\nНе хватает: %d\n", len(failed))
	for _, r := range failed {
		fmt.Fprintf(w, "\n%s\n  %s\n  Как починить: %s\n", r.Name, r.Message, r.Fix)
	}
	fmt.Fprintln(w, "\nОффлайн-поставка: см. SELF_HOSTING.md, раздел «Закрытый контур (offline-поставка)».")
}

func doctorStatusLabel(status string) string {
	switch status {
	case preflight.StatusOK:
		return "есть"
	case preflight.StatusMissing:
		return "нет"
	case preflight.StatusOutdated:
		return "устарел"
	case preflight.StatusSkipped:
		return "не требуется"
	default:
		return status
	}
}
