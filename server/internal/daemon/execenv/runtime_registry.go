package execenv

import (
	"fmt"

	"github.com/adanman/goosar/server/pkg/agent/runtimeregistry"
)

var runtimeRegistry = mustLoadRuntimeRegistry()

func mustLoadRuntimeRegistry() *runtimeregistry.Registry {
	reg, err := runtimeregistry.LoadDefault()
	if err != nil {
		panic(fmt.Sprintf("execenv: load runtime registry: %v", err))
	}
	return reg
}

const (
	runtimeCodeC = "runtime-c"
	runtimeCodeE = "runtime-e"
	runtimeCodeG = "runtime-g"
	runtimeCodeJ = "runtime-j"
	runtimeCodeN = "runtime-n"
)

func runtimeDescriptor(code string) (runtimeregistry.Descriptor, bool) {
	return runtimeRegistry.ByCode(code)
}

func runtimeCLIName(code string) string {
	d, _ := runtimeDescriptor(code)
	return d.CLIName
}

var RuntimeJHomeEnv = runtimeTaskHomeEnv(runtimeCodeJ)

func runtimeTaskHomeEnv(code string) string {
	return runtimeTaskHomeSpec(code).EnvVar
}

func runtimeTaskHomeSpec(code string) runtimeregistry.TaskHomeSpec {
	d, ok := runtimeDescriptor(code)
	if !ok || d.TaskHome == nil {
		return runtimeregistry.TaskHomeSpec{}
	}
	return *d.TaskHome
}
