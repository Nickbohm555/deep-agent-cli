package registry

import (
	"context"
	"reflect"
	"sort"
	"testing"

	"github.com/Nickbohm555/deep-agent-cli/internal/runtime"
)

func TestDefinitionsExposeAllPhaseOneTools(t *testing.T) {
	t.Parallel()

	definitions := Definitions()
	registry := New()

	listed, err := registry.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools returned error: %v", err)
	}

	if len(definitions) != len(listed) {
		t.Fatalf("Definitions returned %d tools while ListTools returned %d", len(definitions), len(listed))
	}

	expected := map[string]struct {
		handlerName string
		properties  []string
	}{
		ReadFileHandlerName: {
			handlerName: ReadFileHandlerName,
			properties:  []string{"path"},
		},
		ListFilesHandlerName: {
			handlerName: ListFilesHandlerName,
			properties:  []string{"path"},
		},
		BashHandlerName: {
			handlerName: BashHandlerName,
			properties:  []string{"command", "working_dir"},
		},
		CodeSearchHandlerName: {
			handlerName: CodeSearchHandlerName,
			properties:  []string{"case_sensitive", "file_type", "path", "pattern"},
		},
		IndexRepoHandlerName: {
			handlerName: IndexRepoHandlerName,
			properties:  []string{},
		},
		IndexStatusHandlerName: {
			handlerName: IndexStatusHandlerName,
			properties:  []string{},
		},
		InspectIndexHandlerName: {
			handlerName: InspectIndexHandlerName,
			properties:  []string{"limit"},
		},
	}

	if len(definitions) != len(expected) {
		t.Fatalf("Definitions returned %d tools, want %d", len(definitions), len(expected))
	}

	for _, tool := range definitions {
		want, ok := expected[tool.Name]
		if !ok {
			t.Fatalf("unexpected tool %q present in static registry", tool.Name)
		}

		assertToolDefinition(t, tool, want.handlerName, want.properties)

		lookedUp, found, err := registry.LookupTool(context.Background(), tool.Name)
		if err != nil {
			t.Fatalf("LookupTool(%q) returned error: %v", tool.Name, err)
		}
		if !found {
			t.Fatalf("LookupTool(%q) did not find a registered tool", tool.Name)
		}
		if lookedUp.Name != tool.Name {
			t.Fatalf("LookupTool(%q) name = %q, want %q", tool.Name, lookedUp.Name, tool.Name)
		}
		if lookedUp.Description != tool.Description {
			t.Fatalf("LookupTool(%q) description mismatch", tool.Name)
		}
		if lookedUp.HandlerName != tool.HandlerName {
			t.Fatalf("LookupTool(%q) handler name = %q, want %q", tool.Name, lookedUp.HandlerName, tool.HandlerName)
		}
		if lookedUp.Handler == nil {
			t.Fatalf("LookupTool(%q) returned nil handler", tool.Name)
		}
		if !reflect.DeepEqual(lookedUp.Schema, tool.Schema) {
			t.Fatalf("LookupTool(%q) schema mismatch", tool.Name)
		}
	}
}

func TestLookupToolReturnsMissingForUnknownTool(t *testing.T) {
	t.Parallel()

	tool, found, err := New().LookupTool(context.Background(), "missing")
	if err != nil {
		t.Fatalf("LookupTool returned error: %v", err)
	}
	if found {
		t.Fatalf("LookupTool unexpectedly found unknown tool: %#v", tool)
	}
	if !reflect.DeepEqual(tool, runtime.ToolDefinition{}) {
		t.Fatalf("LookupTool returned non-zero definition for missing tool: %#v", tool)
	}
}

func assertToolDefinition(t *testing.T, tool runtime.ToolDefinition, handlerName string, expectedProperties []string) {
	t.Helper()

	if tool.Name == "" {
		t.Fatal("tool name should not be empty")
	}
	if tool.Description == "" {
		t.Fatalf("tool %q missing description", tool.Name)
	}
	if tool.HandlerName != handlerName {
		t.Fatalf("tool %q handler name = %q, want %q", tool.Name, tool.HandlerName, handlerName)
	}
	if tool.Handler == nil {
		t.Fatalf("tool %q missing handler", tool.Name)
	}

	assertStrictObjectSchema(t, tool.Name, tool.Schema, expectedProperties)
}

func assertStrictObjectSchema(t *testing.T, toolName string, schema map[string]any, expectedProperties []string) {
	t.Helper()

	if got := schema["type"]; got != "object" {
		t.Fatalf("tool %q schema type = %#v, want object", toolName, got)
	}
	if got := schema["additionalProperties"]; got != false {
		t.Fatalf("tool %q schema additionalProperties = %#v, want false", toolName, got)
	}

	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("tool %q schema properties type = %T, want map[string]any", toolName, schema["properties"])
	}
	if len(properties) != len(expectedProperties) {
		t.Fatalf("tool %q schema property count = %d, want %d", toolName, len(properties), len(expectedProperties))
	}

	gotProperties := []string{}
	for name, rawProperty := range properties {
		gotProperties = append(gotProperties, name)

		property, ok := rawProperty.(map[string]any)
		if !ok {
			t.Fatalf("tool %q property %q type = %T, want map[string]any", toolName, name, rawProperty)
		}
		if _, ok := property["description"].(string); !ok {
			t.Fatalf("tool %q property %q missing description", toolName, name)
		}
		if propertyType, _ := property["type"].(string); propertyType == "object" && property["additionalProperties"] != false {
			t.Fatalf("tool %q nested property %q additionalProperties = %#v, want false", toolName, name, property["additionalProperties"])
		}
	}

	sort.Strings(gotProperties)
	wantProperties := []string{}
	wantProperties = append(wantProperties, expectedProperties...)
	sort.Strings(wantProperties)
	if !reflect.DeepEqual(gotProperties, wantProperties) {
		t.Fatalf("tool %q schema properties = %v, want %v", toolName, gotProperties, wantProperties)
	}

	required := schemaRequiredFields(t, toolName, schema["required"])
	if !reflect.DeepEqual(required, wantProperties) {
		t.Fatalf("tool %q schema required = %v, want %v", toolName, required, wantProperties)
	}
}

func schemaRequiredFields(t *testing.T, toolName string, raw any) []string {
	t.Helper()

	switch required := raw.(type) {
	case []string:
		out := append([]string(nil), required...)
		sort.Strings(out)
		return out
	case []any:
		out := make([]string, 0, len(required))
		for _, item := range required {
			name, ok := item.(string)
			if !ok {
				t.Fatalf("tool %q schema required entry type = %T, want string", toolName, item)
			}
			out = append(out, name)
		}
		sort.Strings(out)
		return out
	default:
		if raw == nil {
			return []string{}
		}
		t.Fatalf("tool %q schema required type = %T, want []string or []any", toolName, raw)
		return nil
	}
}
