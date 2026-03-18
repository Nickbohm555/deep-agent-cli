package registry

import (
	"context"
	"slices"

	"github.com/Nickbohm555/deep-agent-cli/internal/runtime"
)

const (
	ReadFileHandlerName   = "read_file"
	ListFilesHandlerName  = "list_files"
	BashHandlerName       = "bash"
	CodeSearchHandlerName = "code_search"
)

var staticTools = []runtime.ToolDefinition{
	{
		Name:        "read_file",
		Description: "Read the contents of a given relative file path. Use this when you want to see what's inside a file. Do not use this with directory names.",
		Schema:      GenerateSchema[ReadFileInput](),
		HandlerName: ReadFileHandlerName,
	},
	{
		Name:        "list_files",
		Description: "List files and directories at a given path. If the path is empty, list files in the current directory.",
		Schema:      GenerateSchema[ListFilesInput](),
		HandlerName: ListFilesHandlerName,
	},
	{
		Name:        "bash",
		Description: "Execute a bash command and return its output. Use this to run shell commands.",
		Schema:      GenerateSchema[BashInput](),
		HandlerName: BashHandlerName,
	},
	{
		Name:        "code_search",
		Description: "Search for code patterns using ripgrep. Use this to find function definitions, variable usage, or text in the repository.",
		Schema:      GenerateSchema[CodeSearchInput](),
		HandlerName: CodeSearchHandlerName,
	},
}

type StaticRegistry struct{}

func New() StaticRegistry {
	return StaticRegistry{}
}

func (StaticRegistry) ListTools(context.Context) ([]runtime.ToolDefinition, error) {
	return slices.Clone(staticTools), nil
}

func (StaticRegistry) LookupTool(_ context.Context, name string) (runtime.ToolDefinition, bool, error) {
	for _, tool := range staticTools {
		if tool.Name == name {
			return tool, true, nil
		}
	}

	return runtime.ToolDefinition{}, false, nil
}

func Definitions() []runtime.ToolDefinition {
	return slices.Clone(staticTools)
}
