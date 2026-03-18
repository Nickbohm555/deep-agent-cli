package runtime

import (
	"go/ast"
	"go/parser"
	"go/token"
	stdruntime "runtime"
	"path/filepath"
	"reflect"
	"testing"
)

func TestOrchestratorOwnsRuntimeContracts(t *testing.T) {
	t.Parallel()

	var _ TurnRunner = (*Orchestrator)(nil)

	orchestratorType := reflect.TypeOf(Orchestrator{})
	if field, ok := orchestratorType.FieldByName("Provider"); !ok {
		t.Fatal("Orchestrator missing Provider field")
	} else if field.Type != reflect.TypeOf((*ProviderClient)(nil)).Elem() {
		t.Fatalf("Orchestrator.Provider type = %v, want runtime.ProviderClient", field.Type)
	}

	if field, ok := orchestratorType.FieldByName("Registry"); !ok {
		t.Fatal("Orchestrator missing Registry field")
	} else if field.Type != reflect.TypeOf((*Registry)(nil)).Elem() {
		t.Fatalf("Orchestrator.Registry type = %v, want runtime.Registry", field.Type)
	}

	if field, ok := orchestratorType.FieldByName("Dispatcher"); !ok {
		t.Fatal("Orchestrator missing Dispatcher field")
	} else if field.Type != reflect.TypeOf((*ToolDispatcher)(nil)).Elem() {
		t.Fatalf("Orchestrator.Dispatcher type = %v, want runtime.ToolDispatcher", field.Type)
	}
}

func TestRuntimePackageDoesNotImportProvidersOrToolImplementations(t *testing.T) {
	t.Parallel()

	files := []string{"types.go", "orchestrator.go", "interactive.go", "oneshot.go"}
	disallowedImports := map[string]string{
		"github.com/Nickbohm555/deep-agent-cli/internal/provider/openai": "provider adapters belong outside internal/runtime",
		"github.com/Nickbohm555/deep-agent-cli/internal/tools/handlers":   "tool handlers belong outside internal/runtime",
		"github.com/Nickbohm555/deep-agent-cli/internal/tools/registry":   "tool registry implementations belong outside internal/runtime",
	}

	for _, name := range files {
		path := runtimeSourcePath(t, name)
		file := parseRuntimeFile(t, path)

		for _, imported := range file.Imports {
			importPath := imported.Path.Value[1 : len(imported.Path.Value)-1]
			if reason, blocked := disallowedImports[importPath]; blocked {
				t.Fatalf("%s imports %q; %s", name, importPath, reason)
			}
		}
	}
}

func TestDriversDependOnTurnRunnerContract(t *testing.T) {
	t.Parallel()

	assertStructFieldTypeName(t, "interactive.go", "InteractiveDriver", "Runner", "TurnRunner")
	assertStructFieldTypeName(t, "oneshot.go", "OneShotDriver", "Runner", "TurnRunner")
}

func assertStructFieldTypeName(t *testing.T, fileName, structName, fieldName, wantType string) {
	t.Helper()

	file := parseRuntimeFile(t, runtimeSourcePath(t, fileName))
	spec := findTypeSpec(t, file, structName)

	structType, ok := spec.Type.(*ast.StructType)
	if !ok {
		t.Fatalf("%s in %s is %T, want *ast.StructType", structName, fileName, spec.Type)
	}

	for _, field := range structType.Fields.List {
		for _, name := range field.Names {
			if name.Name != fieldName {
				continue
			}

			ident, ok := field.Type.(*ast.Ident)
			if !ok {
				t.Fatalf("%s.%s type in %s = %T, want identifier %q", structName, fieldName, fileName, field.Type, wantType)
			}
			if ident.Name != wantType {
				t.Fatalf("%s.%s type in %s = %q, want %q", structName, fieldName, fileName, ident.Name, wantType)
			}
			return
		}
	}

	t.Fatalf("%s.%s not found in %s", structName, fieldName, fileName)
}

func findTypeSpec(t *testing.T, file *ast.File, typeName string) *ast.TypeSpec {
	t.Helper()

	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}
		for _, spec := range gen.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if ok && typeSpec.Name.Name == typeName {
				return typeSpec
			}
		}
	}

	t.Fatalf("type %s not found", typeName)
	return nil
}

func parseRuntimeFile(t *testing.T, path string) *ast.File {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("ParseFile(%s) returned error: %v", path, err)
	}

	return file
}

func runtimeSourcePath(t *testing.T, name string) string {
	t.Helper()

	_, file, _, ok := stdruntime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}

	return filepath.Join(filepath.Dir(file), name)
}
