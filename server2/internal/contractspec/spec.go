// Package contractspec читает docs/50-api-contract.yaml и перечисляет все
// операции (метод+путь+operationId). Используется генератором
// server2/tools/genstubs (пишет server2/internal/app/stubs_gen.go) и тестом
// покрытия маршрутов (server2/internal/app/routes_test.go), чтобы обе стороны
// (генерация и проверка) читали спецификацию одним и тем же кодом.
package contractspec

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Operation — одна операция контракта.
type Operation struct {
	Method      string
	Path        string
	OperationID string
}

var httpMethods = []string{"get", "post", "put", "patch", "delete", "options", "head"}

// Load разбирает paths: контракта в отсортированный (path, затем method)
// список операций.
func Load(specPath string) ([]Operation, error) {
	data, err := os.ReadFile(specPath)
	if err != nil {
		return nil, fmt.Errorf("genstubs: чтение %s: %w", specPath, err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("genstubs: разбор YAML: %w", err)
	}
	pathsRaw, ok := doc["paths"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("genstubs: в спецификации нет paths:")
	}

	var ops []Operation
	for path, itemRaw := range pathsRaw {
		item, ok := itemRaw.(map[string]any)
		if !ok {
			continue
		}
		for _, m := range httpMethods {
			opRaw, ok := item[m]
			if !ok {
				continue
			}
			opMap, _ := opRaw.(map[string]any)
			opID, _ := opMap["operationId"].(string)
			ops = append(ops, Operation{
				Method:      strings.ToUpper(m),
				Path:        path,
				OperationID: opID,
			})
		}
	}
	sort.Slice(ops, func(i, j int) bool {
		if ops[i].Path != ops[j].Path {
			return ops[i].Path < ops[j].Path
		}
		return ops[i].Method < ops[j].Method
	})
	return ops, nil
}
