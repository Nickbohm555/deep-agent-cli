package registry

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Nickbohm555/deep-agent-cli/internal/runtime"
	"github.com/Nickbohm555/deep-agent-cli/internal/tools/handlers"
)

func TestRegistryListsExpectedTools(t *testing.T) {
	registry := New()

	tools, err := registry.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools returned error: %v", err)
	}

	if len(tools) != 7 {
		t.Fatalf("ListTools returned %d tools, want 7", len(tools))
	}

	expected := map[string]string{
		"read_file":     ReadFileHandlerName,
		"list_files":    ListFilesHandlerName,
		"bash":          BashHandlerName,
		"code_search":   CodeSearchHandlerName,
		"index_repo":    IndexRepoHandlerName,
		"index_status":  IndexStatusHandlerName,
		"inspect_index": InspectIndexHandlerName,
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
		if tool.Handler == nil {
			t.Fatalf("tool %s handler should be bound for task 3", tool.Name)
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
	if tool.Handler == nil {
		t.Fatal("LookupTool returned nil handler for code_search")
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
	schema := GenerateSchema[handlers.CodeSearchInput]()

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

func TestRegistryHandlersExecuteViaLookup(t *testing.T) {
	registry := New()
	tempDir := t.TempDir()

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd returned error: %v", err)
	}
	t.Cleanup(func() {
		if chdirErr := os.Chdir(originalWD); chdirErr != nil {
			t.Fatalf("failed to restore working directory: %v", chdirErr)
		}
	})

	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Chdir returned error: %v", err)
	}

	if err := os.WriteFile("sample.txt", []byte("hello from handler"), 0o644); err != nil {
		t.Fatalf("WriteFile sample.txt returned error: %v", err)
	}
	if err := os.MkdirAll(filepath.Join("subdir", ".devenv"), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join("subdir", "main.go"), []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile main.go returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join("subdir", ".devenv", "ignored.txt"), []byte("skip"), 0o644); err != nil {
		t.Fatalf("WriteFile ignored.txt returned error: %v", err)
	}

	tests := []struct {
		name       string
		toolName   string
		args       any
		assertions func(t *testing.T, result runtime.ToolResult)
	}{
		{
			name:     "read_file",
			toolName: "read_file",
			args: handlers.ReadFileInput{
				Path: "sample.txt",
			},
			assertions: func(t *testing.T, result runtime.ToolResult) {
				if result.Output != "hello from handler" {
					t.Fatalf("read_file output = %q, want %q", result.Output, "hello from handler")
				}
				if result.IsError {
					t.Fatal("read_file should not mark result as error")
				}
			},
		},
		{
			name:     "list_files",
			toolName: "list_files",
			args: handlers.ListFilesInput{
				Path: ".",
			},
			assertions: func(t *testing.T, result runtime.ToolResult) {
				var files []string
				if err := json.Unmarshal([]byte(result.Output), &files); err != nil {
					t.Fatalf("list_files output is not valid json: %v", err)
				}
				joined := strings.Join(files, ",")
				if !strings.Contains(joined, "sample.txt") {
					t.Fatalf("list_files output missing sample.txt: %v", files)
				}
				if strings.Contains(joined, ".devenv") {
					t.Fatalf("list_files output should skip .devenv entries: %v", files)
				}
			},
		},
		{
			name:     "bash",
			toolName: "bash",
			args: handlers.BashInput{
				Command: "printf 'bash ok'",
			},
			assertions: func(t *testing.T, result runtime.ToolResult) {
				if result.Output != "bash ok" {
					t.Fatalf("bash output = %q, want %q", result.Output, "bash ok")
				}
				if result.IsError {
					t.Fatal("bash should not mark result as error")
				}
			},
		},
		{
			name:     "code_search",
			toolName: "code_search",
			args: handlers.CodeSearchInput{
				Pattern:  "func main",
				Path:     "subdir",
				FileType: "go",
			},
			assertions: func(t *testing.T, result runtime.ToolResult) {
				if !strings.Contains(result.Output, "subdir/main.go:2:func main() {}") {
					t.Fatalf("code_search output missing expected match: %q", result.Output)
				}
				if result.IsError {
					t.Fatal("code_search should not mark result as error")
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tool, ok, err := registry.LookupTool(context.Background(), tc.toolName)
			if err != nil {
				t.Fatalf("LookupTool returned error: %v", err)
			}
			if !ok {
				t.Fatalf("LookupTool did not find %s", tc.toolName)
			}

			rawArgs, err := json.Marshal(tc.args)
			if err != nil {
				t.Fatalf("Marshal returned error: %v", err)
			}

			ctx, err := runtime.WithRepoRoot(context.Background(), tempDir)
			if err != nil {
				t.Fatalf("WithRepoRoot returned error: %v", err)
			}

			result, err := tool.Handler(ctx, runtime.ToolCall{
				ID:        "call-1",
				Name:      tc.toolName,
				Arguments: rawArgs,
			})
			if err != nil {
				t.Fatalf("handler returned error: %v", err)
			}
			if result.CallID != "call-1" {
				t.Fatalf("result CallID = %q, want call-1", result.CallID)
			}
			if result.Name != tc.toolName {
				t.Fatalf("result Name = %q, want %q", result.Name, tc.toolName)
			}

			tc.assertions(t, result)
		})
	}
}
