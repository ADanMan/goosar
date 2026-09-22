package main

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"

	"github.com/spf13/cobra"
)

func init() {
	versionCmd.Flags().String("output", "text", "Формат вывода: text или json")
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Показать сведения о версии",
	RunE:  runVersion,
}

func runVersion(cmd *cobra.Command, _ []string) error {
	output, _ := cmd.Flags().GetString("output")

	if output == "json" {
		info := map[string]string{
			"version": version,
			"commit":  commit,
			"date":    date,
			"go":      runtime.Version(),
			"os":      runtime.GOOS,
			"arch":    runtime.GOARCH,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(info)
	}

	fmt.Printf("goosar %s (commit: %s, built: %s)\n", version, commit, date)
	fmt.Printf("go: %s, os/arch: %s/%s\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)
	return nil
}
