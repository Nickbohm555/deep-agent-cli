package registry

import (
	"context"
	"reflect"
	"testing"
)

func TestRegistryListsExpectedTools(t *testing.T) {
	registry := New()

	tools, err := registry.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools returned error: %v", err)
	}

	if len(tools) != 4 {
		t.Fatalf("ListTools returned %d tools, want 4", len(tools))
	}

	expected := map[string]string{
		"read_file":   ReadFileHandlerName,
		"list_files":  ListFilesHandlerName,
		"bash":        BashHandlerName,
		"code_search": CodeSearchHandlerName,
	}

	for _, tool := range tools {
		handlerName, ok := expected[tool.Name]
		if !ok {
			t.Fatalf("unexpected tool registered: %s", tool.Name)
		}
		if tool.Description == "" {
			t.Fatalf("tool %s missing description", tool.Name)
		}
		if tool.HandlerName != handlerName {
			t.Fatalf("tool %s handler name = %q, want %q", tool.Name, tool.HandlerName, handlerName)
		}
		if tool.Handler != nil {
			t.Fatalf("tool %s handler should not be bound before task 3", tool.Name)
		}
		if len(tool.Schema) == 0 {
			t.Fatalf("tool %s missing schema", tool.Name)
		}
	}
}

func TestRegistryLookupTool(t *testing.T) {
	registry := New()

	tool, ok, err := registry.LookupTool(context.Background(), "code_search")
	if err != nil {
		t.Fatalf("LookupTool returned error: %v", err)
	}
	if !ok {
		t.Fatal("LookupTool did not find code_search")
	}
	if tool.HandlerName != CodeSearchHandlerName {
		t.Fatalf("LookupTool handler name = %q, want %q", tool.HandlerName, CodeSearchHandlerName)
	}

	_, ok, err = registry.LookupTool(context.Background(), "missing")
	if err != nil {
		t.Fatalf("LookupTool missing returned error: %v", err)
	}
	if ok {
		t.Fatal("LookupTool unexpectedly found missing tool")
	}
}

func TestSchemaGenerationIsStrict(t *testing.T) {
	schema := GenerateSchema[CodeSearchInput]()

	if got := schema["type"]; got != "object" {
		t.Fatalf("schema type = %#v, want object", got)
	}
	if got := schema["additionalProperties"]; got != false {
		t.Fatalf("schema additionalProperties = %#v, want false", got)
	}

	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema properties type = %T, want map[string]any", schema["properties"])
	}

	var gotRequired []string
	switch required := schema["required"].(type) {
	case []string:
		gotRequired = append(gotRequired, required...)
	case []any:
		gotRequired = make([]string, 0, len(required))
		for _, name := range required {
			gotRequired = append(gotRequired, name.(string))
		}
	default:
		t.Fatalf("schema required type = %T, want []string or []any", schema["required"])
	}

	wantRequired := []string{"case_sensitive", "file_type", "path", "pattern"}
	if !reflect.DeepEqual(gotRequired, wantRequired) {
		t.Fatalf("schema required = %v, want %v", gotRequired, wantRequired)
	}

	for name, rawProperty := range properties {
		property, ok := rawProperty.(map[string]any)
		if !ok {
			t.Fatalf("property %s type = %T, want map[string]any", name, rawProperty)
		}
		if nestedType, _ := property["type"].(string); nestedType == "object" {
			if got := property["additionalProperties"]; got != false {
				t.Fatalf("nested property %s additionalProperties = %#v, want false", name, got)
			}
		}
	}
}
