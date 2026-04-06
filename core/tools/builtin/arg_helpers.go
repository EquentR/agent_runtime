package builtin

import (
	"fmt"
	"strings"
)

func requiredStringArg(arguments map[string]any, key string) (string, error) {
	value, ok, err := optionalStringArg(arguments, key)
	if err != nil {
		return "", err
	}
	if !ok || value == "" {
		return "", fmt.Errorf("%s is required", key)
	}
	return value, nil
}

func optionalStringArg(arguments map[string]any, key string) (string, bool, error) {
	value, ok := arguments[key]
	if !ok || value == nil {
		return "", false, nil
	}
	s, ok := value.(string)
	if !ok {
		return "", false, fmt.Errorf("%s must be a string", key)
	}
	return strings.TrimSpace(s), true, nil
}

func boolArg(arguments map[string]any, key string, defaultValue bool) (bool, error) {
	value, ok := arguments[key]
	if !ok || value == nil {
		return defaultValue, nil
	}
	b, ok := value.(bool)
	if !ok {
		return false, fmt.Errorf("%s must be a boolean", key)
	}
	return b, nil
}

func optionalIntArg(arguments map[string]any, key string) (int, bool, error) {
	value, ok := arguments[key]
	if !ok || value == nil {
		return 0, false, nil
	}
	parsed, err := coerceIntArg(value, key)
	if err != nil {
		return 0, false, err
	}
	return parsed, true, nil
}

func intArg(arguments map[string]any, key string, defaultValue int) (int, error) {
	value, ok, err := optionalIntArg(arguments, key)
	if err != nil {
		return 0, err
	}
	if !ok {
		return defaultValue, nil
	}
	return value, nil
}

func coerceIntArg(value any, key string) (int, error) {
	switch typed := value.(type) {
	case int:
		return typed, nil
	case int32:
		return int(typed), nil
	case int64:
		return int(typed), nil
	case float64:
		return int(typed), nil
	case float32:
		return int(typed), nil
	default:
		return 0, fmt.Errorf("%s must be an integer", key)
	}
}

func stringSliceArg(arguments map[string]any, key string) ([]string, error) {
	value, ok := arguments[key]
	if !ok || value == nil {
		return nil, nil
	}
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...), nil
	case []any:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("%s items must be strings", key)
			}
			result = append(result, s)
		}
		return result, nil
	default:
		return nil, fmt.Errorf("%s must be an array of strings", key)
	}
}

func stringMapArg(arguments map[string]any, key string) (map[string]string, error) {
	value, ok := arguments[key]
	if !ok || value == nil {
		return nil, nil
	}
	switch typed := value.(type) {
	case map[string]string:
		result := make(map[string]string, len(typed))
		for k, v := range typed {
			result[k] = v
		}
		return result, nil
	case map[string]any:
		result := make(map[string]string, len(typed))
		for k, v := range typed {
			result[k] = fmt.Sprint(v)
		}
		return result, nil
	default:
		return nil, fmt.Errorf("%s must be an object", key)
	}
}
