package actions

import (
	"encoding/json"
	"fmt"
)

// objectSchema builds a strict object schema, mirroring the helper the agent tool definitions use.
// Required stays an empty array rather than nil, because encoding/json renders a nil string slice as
// "required": null, which providers reject as invalid.
func objectSchema(properties map[string]any, required ...string) map[string]any {
	if properties == nil {
		properties = map[string]any{}
	}
	if required == nil {
		required = []string{}
	}
	return map[string]any{
		"type":                 "object",
		"properties":           properties,
		"required":             required,
		"additionalProperties": false,
	}
}

func stringSchema() map[string]any         { return map[string]any{"type": "string", "minLength": 1} }
func nullableStringSchema() map[string]any { return map[string]any{"type": []string{"string", "null"}} }
func booleanSchema() map[string]any        { return map[string]any{"type": "boolean"} }
func integerSchema(minimum, maximum int) map[string]any {
	return map[string]any{"type": "integer", "minimum": minimum, "maximum": maximum}
}
func enumSchema(values ...string) map[string]any {
	return map[string]any{"type": "string", "enum": values}
}
func stringArraySchema() map[string]any {
	return map[string]any{"type": "array", "items": stringSchema()}
}

func pathReferenceSchema() map[string]any {
	return objectSchema(map[string]any{
		"rootId": stringSchema(), "relativePath": stringSchema(),
	}, "rootId", "relativePath")
}

func pathReferenceArraySchema() map[string]any {
	return map[string]any{"type": "array", "minItems": 1, "items": pathReferenceSchema()}
}

func pathReferenceListSchema() map[string]any {
	return map[string]any{"type": "array", "items": pathReferenceSchema()}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func validateActionSchema(schema map[string]any, value any, path string) error {
	typeName, _ := schema["type"].(string)
	switch typeName {
	case "object":
		objectValue, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("%s must be an object", path)
		}
		properties, _ := schema["properties"].(map[string]any)
		if required, ok := schema["required"].([]string); ok {
			for _, name := range required {
				if _, exists := objectValue[name]; !exists {
					return fmt.Errorf("%s.%s is required", path, name)
				}
			}
		} else if required, ok := schema["required"].([]any); ok {
			for _, item := range required {
				name, _ := item.(string)
				if _, exists := objectValue[name]; name != "" && !exists {
					return fmt.Errorf("%s.%s is required", path, name)
				}
			}
		}
		for name, child := range objectValue {
			childSchema, exists := properties[name]
			if !exists {
				if schema["additionalProperties"] == false {
					return fmt.Errorf("%s.%s is not allowed", path, name)
				}
				continue
			}
			if typedSchema, ok := childSchema.(map[string]any); ok && len(typedSchema) > 0 {
				if err := validateActionSchema(typedSchema, child, path+"."+name); err != nil {
					return err
				}
			}
		}
	case "string":
		stringValue, ok := value.(string)
		if !ok {
			return fmt.Errorf("%s must be a string", path)
		}
		if minimum, ok := schema["minLength"].(int); ok && len(stringValue) < minimum {
			return fmt.Errorf("%s must not be empty", path)
		}
		if enum, ok := schema["enum"].([]string); ok && !containsString(enum, stringValue) {
			return fmt.Errorf("%s has unsupported value %q", path, stringValue)
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("%s must be a boolean", path)
		}
	case "integer":
		number, ok := value.(json.Number)
		if !ok {
			return fmt.Errorf("%s must be an integer", path)
		}
		integer, err := number.Int64()
		if err != nil {
			return fmt.Errorf("%s must be an integer", path)
		}
		if minimum, ok := schema["minimum"].(int); ok && integer < int64(minimum) {
			return fmt.Errorf("%s must be at least %d", path, minimum)
		}
		if maximum, ok := schema["maximum"].(int); ok && integer > int64(maximum) {
			return fmt.Errorf("%s must be at most %d", path, maximum)
		}
	case "number":
		number, ok := value.(json.Number)
		if !ok {
			return fmt.Errorf("%s must be a number", path)
		}
		decimal, err := number.Float64()
		if err != nil {
			return fmt.Errorf("%s must be a number", path)
		}
		if minimum, ok := schema["minimum"].(float64); ok && decimal < minimum {
			return fmt.Errorf("%s must be at least %g", path, minimum)
		}
		if maximum, ok := schema["maximum"].(float64); ok && decimal > maximum {
			return fmt.Errorf("%s must be at most %g", path, maximum)
		}
	case "array":
		items, ok := value.([]any)
		if !ok {
			return fmt.Errorf("%s must be an array", path)
		}
		if minimum, ok := schema["minItems"].(int); ok && len(items) < minimum {
			return fmt.Errorf("%s must contain at least %d item(s)", path, minimum)
		}
		if maximum, ok := schema["maxItems"].(int); ok && len(items) > maximum {
			return fmt.Errorf("%s must contain at most %d item(s)", path, maximum)
		}
		itemSchema, _ := schema["items"].(map[string]any)
		for index, item := range items {
			if err := validateActionSchema(itemSchema, item, fmt.Sprintf("%s[%d]", path, index)); err != nil {
				return err
			}
		}
	case "":
		if types, ok := schema["type"].([]string); ok {
			if value == nil && containsString(types, "null") {
				return nil
			}
			if stringValue, ok := value.(string); ok && containsString(types, "string") {
				_ = stringValue
				return nil
			}
			return fmt.Errorf("%s has an unsupported type", path)
		}
	}
	return nil
}
