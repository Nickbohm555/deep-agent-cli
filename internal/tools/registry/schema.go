package registry

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/invopop/jsonschema"
)

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
