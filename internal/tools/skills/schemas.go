package skills

import (
	"fmt"
	"math"
	"reflect"
	"sort"

	"local-agent/internal/core"
)

// UploadInput is the payload for skill registration.
type UploadInput struct {
	Path        string `json:"path"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// SkillRunInput is the payload accepted by the skill.run tool.
type SkillRunInput struct {
	SkillID string         `json:"skill_id"`
	Args    map[string]any `json:"args"`
}

// SkillRunResult is the normalized executor result for a skill invocation.
type SkillRunResult struct {
	SkillID    string         `json:"skill_id"`
	Status     string         `json:"status"`
	Output     map[string]any `json:"output"`
	Stdout     string         `json:"stdout,omitempty"`
	Stderr     string         `json:"stderr,omitempty"`
	ExitCode   int            `json:"exit_code"`
	DurationMS int64          `json:"duration_ms"`
}

// UploadZipResponse describes the install result for a managed zip package.
type UploadZipResponse struct {
	Skill   core.SkillRegistration `json:"skill"`
	Package core.SkillPackageInfo  `json:"package"`
}

// SkillValidateResponse is the API response for preflight validation.
type SkillValidateResponse struct {
	Status     string                 `json:"status"`
	Skill      core.SkillRegistration `json:"skill"`
	Package    core.SkillPackageInfo  `json:"package"`
	Validation ValidationResult       `json:"validation"`
}

// ValidateSchemaDocument validates the supported subset of JSON Schema used by skills.
func ValidateSchemaDocument(schema map[string]any) error {
	if len(schema) == 0 {
		return nil
	}
	return validateSchemaNode(schema, "$")
}

// ValidateInput validates runtime args against the manifest input schema.
func ValidateInput(schema map[string]any, input map[string]any) error {
	if len(schema) == 0 {
		return nil
	}
	return validateValueAgainstSchema(schema, input, "$")
}

func validateSchemaNode(schema map[string]any, path string) error {
	typeName, _ := schema["type"].(string)
	if typeName == "" {
		return fmt.Errorf("%s.type is required", path)
	}

	switch typeName {
	case "object":
		if rawProps, ok := schema["properties"]; ok {
			props, err := mapValues(rawProps)
			if err != nil {
				return fmt.Errorf("%s.properties: %w", path, err)
			}
			keys := make([]string, 0, len(props))
			for key := range props {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				child, err := mapValues(props[key])
				if err != nil {
					return fmt.Errorf("%s.properties.%s: %w", path, key, err)
				}
				if err := validateSchemaNode(child, path+".properties."+key); err != nil {
					return err
				}
			}
		}
		if rawRequired, ok := schema["required"]; ok {
			if _, err := stringSlice(rawRequired); err != nil {
				return fmt.Errorf("%s.required: %w", path, err)
			}
		}
	case "array":
		if rawItems, ok := schema["items"]; ok {
			child, err := mapValues(rawItems)
			if err != nil {
				return fmt.Errorf("%s.items: %w", path, err)
			}
			if err := validateSchemaNode(child, path+".items"); err != nil {
				return err
			}
		}
	case "string", "integer", "number", "boolean":
	default:
		return fmt.Errorf("%s.type %q is unsupported", path, typeName)
	}
	return nil
}

func validateValueAgainstSchema(schema map[string]any, value any, path string) error {
	typeName, _ := schema["type"].(string)
	switch typeName {
	case "object":
		obj, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("%s must be an object", path)
		}
		required, err := stringSlice(schema["required"])
		if err != nil {
			return fmt.Errorf("%s.required: %w", path, err)
		}
		for _, key := range required {
			if _, ok := obj[key]; !ok {
				return fmt.Errorf("%s.%s is required", path, key)
			}
		}
		props, err := mapValues(schema["properties"])
		if err != nil {
			return fmt.Errorf("%s.properties: %w", path, err)
		}
		for key, item := range obj {
			childSchema, ok := props[key]
			if !ok {
				continue
			}
			child, err := mapValues(childSchema)
			if err != nil {
				return fmt.Errorf("%s.%s: %w", path, key, err)
			}
			if err := validateValueAgainstSchema(child, item, path+"."+key); err != nil {
				return err
			}
		}
	case "array":
		if _, ok := value.([]any); ok {
			return nil
		}
		rv := reflect.ValueOf(value)
		if !rv.IsValid() || (rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array) {
			return fmt.Errorf("%s must be an array", path)
		}
	case "string":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("%s must be a string", path)
		}
	case "integer":
		if !isInteger(value) {
			return fmt.Errorf("%s must be an integer", path)
		}
	case "number":
		if !isNumber(value) {
			return fmt.Errorf("%s must be a number", path)
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("%s must be a boolean", path)
		}
	default:
		return fmt.Errorf("%s.type %q is unsupported", path, typeName)
	}
	return nil
}

func mapValues(raw any) (map[string]any, error) {
	if raw == nil {
		return map[string]any{}, nil
	}
	switch value := raw.(type) {
	case map[string]any:
		return value, nil
	default:
		return nil, fmt.Errorf("must be an object")
	}
}

func stringSlice(raw any) ([]string, error) {
	if raw == nil {
		return nil, nil
	}
	switch value := raw.(type) {
	case []string:
		return append([]string(nil), value...), nil
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			text, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("must contain only strings")
			}
			out = append(out, text)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("must be an array of strings")
	}
}

func isInteger(value any) bool {
	switch number := value.(type) {
	case int, int8, int16, int32, int64:
		return true
	case uint, uint8, uint16, uint32, uint64:
		return true
	case float32:
		return math.Trunc(float64(number)) == float64(number)
	case float64:
		return math.Trunc(number) == number
	default:
		return false
	}
}

func isNumber(value any) bool {
	switch value.(type) {
	case int, int8, int16, int32, int64:
		return true
	case uint, uint8, uint16, uint32, uint64:
		return true
	case float32, float64:
		return true
	default:
		return false
	}
}
