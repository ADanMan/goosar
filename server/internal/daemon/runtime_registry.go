package daemon

import (
	"fmt"
	"strings"

	"github.com/adanman/goosar/server/pkg/agent/runtimeregistry"
)

var runtimeRegistry = mustLoadRuntimeRegistry()

func mustLoadRuntimeRegistry() *runtimeregistry.Registry {
	reg, err := runtimeregistry.LoadDefault()
	if err != nil {
		panic(fmt.Sprintf("daemon: load runtime registry: %v", err))
	}
	return reg
}

const (
	runtimeCodeC = "runtime-c"
	runtimeCodeD = "runtime-d"
	runtimeCodeE = "runtime-e"
	runtimeCodeG = "runtime-g"
	runtimeCodeJ = "runtime-j"
	runtimeCodeK = "runtime-k"
	runtimeCodeM = "runtime-m"
	runtimeCodeN = "runtime-n"
	runtimeCodeQ = "runtime-q"
	runtimeCodeR = "runtime-r"
)

func runtimeDescriptor(code string) (runtimeregistry.Descriptor, bool) {
	return runtimeRegistry.ByCode(code)
}

func runtimeCLIName(code string) string {
	d, _ := runtimeDescriptor(code)
	return d.CLIName
}

func runtimeDisplayName(code string) string {
	if code == "" {
		return code
	}
	if d, ok := runtimeDescriptor(code); ok && d.DisplayName != "" {
		return d.DisplayName
	}
	return code
}

func runtimeEnvPrefix(code string) string {
	return "GOOSAR_RUNTIME_" + strings.ToUpper(strings.TrimPrefix(code, "runtime-"))
}
