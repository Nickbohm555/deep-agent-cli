package registry

import (
	"context"
	"slices"

	"github.com/Nickbohm555/deep-agent-cli/internal/runtime"
	"github.com/Nickbohm555/deep-agent-cli/internal/tools/handlers"
)

const (
	ReadFileHandlerName       = "read_file"
	ListFilesHandlerName      = "list_files"
	BashHandlerName           = "bash"
	CodeSearchHandlerName     = "code_search"
	IndexRepoHandlerName      = "index_repo"
	IndexStatusHandlerName    = "index_status"
	InspectIndexHandlerName   = "inspect_index"
	SemanticSearchHandlerName = "semantic_search"
)

var staticTools = []runtime.ToolDefinition{
	{
		Name:        "read_file",
		Description: "Read the contents of a given relative file path. Use this when you want to see what's inside a file. Do not use this with directory names.",
		Schema:      GenerateSchema[handlers.ReadFileInput](),
		Handler:     handlers.ReadFile,
		HandlerName: ReadFileHandlerName,
	},
	{
		Name:        "list_files",
		Description: "List files and directories at a given path. If the path is empty, list files in the current directory.",
		Schema:      GenerateSchema[handlers.ListFilesInput](),
		Handler:     handlers.ListFiles,
		HandlerName: ListFilesHandlerName,
	},
	{
		Name:        "bash",
		Description: "Execute a bash command and return its output. Use this to run shell commands.",
		Schema:      GenerateSchema[handlers.BashInput](),
		Handler:     handlers.Bash,
		HandlerName: BashHandlerName,
	},
	{
		Name:        "code_search",
		Description: "Search for code patterns using ripgrep. Use this to find function definitions, variable usage, or text in the repository.",
		Schema:      GenerateSchema[handlers.CodeSearchInput](),
		Handler:     handlers.CodeSearch,
		HandlerName: CodeSearchHandlerName,
	},
	{
		Name:        "index_repo",
		Description: "Run a full baseline index rebuild for the current session-bound repository and persist embeddings for discovered chunks.",
		Schema:      GenerateSchema[handlers.IndexRepoInput](),
		Handler:     handlers.IndexRepo,
		HandlerName: IndexRepoHandlerName,
	},
	{
		Name:        "index_status",
		Description: "Inspect background sync and indexing progress for the current session-bound repository, including queue state, latest success timestamps, and recent failures.",
		Schema:      GenerateSchema[handlers.IndexStatusInput](),
		Handler:     handlers.IndexStatus,
		HandlerName: IndexStatusHandlerName,
	},
	{
		Name:        "inspect_index",
		Description: "Inspect persisted index rows for the current session-bound repository, including file path, chunk ordering, and embedding metadata.",
		Schema:      GenerateSchema[handlers.InspectIndexInput](),
		Handler:     handlers.InspectIndex,
		HandlerName: InspectIndexHandlerName,
	},
	{
		Name:        "semantic_search",
		Description: "Run semantic retrieval against the current session-bound repository and return ranked chunk evidence with file path, score, and snippet metadata.",
		Schema:      GenerateSchema[handlers.SemanticSearchInput](),
		Handler:     handlers.SemanticSearch,
		HandlerName: SemanticSearchHandlerName,
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
