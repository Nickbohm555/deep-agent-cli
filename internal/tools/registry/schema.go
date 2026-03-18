package registry

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/invopop/jsonschema"
)

type ReadFileInput struct {
	Path string `json:"path" jsonschema_description:"The relative path of a file in the working directory."`
}

type ListFilesInput struct {
	Path string `json:"path,omitempty" jsonschema_description:"Relative path to list from. Use an empty string for the current directory."`
}

type BashInput struct {
	Command string `json:"command" jsonschema_description:"The bash command to execute."`
}

type CodeSearchInput struct {
	Pattern       string `json:"pattern" jsonschema_description:"The search pattern or regex to look for."`
	Path          string `json:"path,omitempty" jsonschema_description:"Path to search in. Use an empty string to search the current directory."`
	FileType      string `json:"file_type,omitempty" jsonschema_description:"Optional file extension or ripgrep type filter. Use an empty string for no filter."`
	CaseSensitive bool   `json:"case_sensitive,omitempty" jsonschema_description:"Whether the search should be case sensitive."`
}

func GenerateSchema[T any]() map[string]any {
	reflector := jsonschema.Reflector{
		AllowAdditionalProperties: false,
		DoNotReference:            true,
	}

	var input T
	schema := reflector.Reflect(input)

	payload, err := json.Marshal(schema)
	if err != nil {
		panic(fmt.Errorf("marshal generated schema: %w", err))
	}

	var out map[string]any
	if err := json.Unmarshal(payload, &out); err != nil {
		panic(fmt.Errorf("unmarshal generated schema: %w", err))
	}

	normalizeStrictObjectSchema(out)
	return out
}

func normalizeStrictObjectSchema(schema map[string]any) {
	schemaType, _ := schema["type"].(string)
	if schemaType == "object" {
		schema["additionalProperties"] = false

		properties, _ := schema["properties"].(map[string]any)
		if len(properties) > 0 {
			required := make([]string, 0, len(properties))
			for name, property := range properties {
				required = append(required, name)
				if nested, ok := property.(map[string]any); ok {
					normalizeStrictObjectSchema(nested)
				}
			}
			sort.Strings(required)
			schema["required"] = required
		}
	}

	for _, key := range []string{"items", "additionalProperties"} {
		nested, ok := schema[key].(map[string]any)
		if ok {
			normalizeStrictObjectSchema(nested)
		}
	}

	allOf, _ := schema["allOf"].([]any)
	for _, item := range allOf {
		if nested, ok := item.(map[string]any); ok {
			normalizeStrictObjectSchema(nested)
		}
	}

	anyOf, _ := schema["anyOf"].([]any)
	for _, item := range anyOf {
		if nested, ok := item.(map[string]any); ok {
			normalizeStrictObjectSchema(nested)
		}
	}

	oneOf, _ := schema["oneOf"].([]any)
	for _, item := range oneOf {
		if nested, ok := item.(map[string]any); ok {
			normalizeStrictObjectSchema(nested)
		}
	}
}
