// Package postmanenv parses Postman Environment v2.1 JSON exports.
//
// Postman environments contain more metadata than postkid needs. The parser
// keeps only enabled, scalar values and maps them to postkid's Environment
// model. Unknown Postman metadata is intentionally ignored so exports from
// different Postman versions remain importable.
package postmanenv

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode"

	"go.planetmeican.com/yangguang/postkid/internal/model"
)

// Result is the result of importing one Postman environment.
type Result struct {
	Environment model.Environment
	Imported    int
}

// Parse imports one Postman Environment v2.1 JSON document.
//
// A Postman environment export has a root name and a values array. Values
// marked enabled=false are omitted, while value (or initialValue when value
// is absent/null) is converted to a scalar string. The result always owns a
// fresh variables map.
func Parse(data []byte) (Result, error) {
	root, err := decodeRoot(data)
	if err != nil {
		return Result{}, err
	}

	name, err := requiredString(root, "name", "environment")
	if err != nil {
		return Result{}, err
	}
	name, err = normalizeName(name, "environment.name")
	if err != nil {
		return Result{}, err
	}

	valuesRaw, ok := root["values"]
	if !ok {
		return Result{}, importError("environment.values", "field is required")
	}
	variables, err := parseValues(valuesRaw, "environment.values")
	if err != nil {
		return Result{}, err
	}

	return Result{
		Environment: model.Environment{Name: name, Variables: variables},
		Imported:    len(variables),
	}, nil
}

// ParseEnvironment is an explicit alias for Parse for callers that prefer a
// name identifying the accepted document type.
func ParseEnvironment(data []byte) (Result, error) { return Parse(data) }

func parseValues(raw json.RawMessage, path string) (map[string]string, error) {
	items, err := decodeArray(raw, path)
	if err != nil {
		return nil, err
	}
	variables := make(map[string]string, len(items))
	for i, itemRaw := range items {
		itemPath := fmt.Sprintf("%s[%d]", path, i)
		item, err := decodeObject(itemRaw, itemPath)
		if err != nil {
			return nil, err
		}
		enabled, err := optionalBool(item, "enabled", itemPath)
		if err != nil {
			return nil, err
		}
		if !enabled {
			// Disabled values are not part of the runtime environment. Match
			// Postman's export semantics and do not validate fields that are
			// irrelevant once the value is disabled.
			continue
		}

		key, err := requiredString(item, "key", itemPath)
		if err != nil {
			return nil, err
		}
		key = strings.TrimSpace(key)
		if key == "" {
			return nil, importError(itemPath+".key", "must not be empty")
		}
		if !validVariableName(key) {
			return nil, importError(itemPath+".key", "variable name %q is not supported; use letters, digits, or underscore", key)
		}
		if _, exists := variables[key]; exists {
			return nil, importError(itemPath, "duplicate environment variable %q", key)
		}

		valueRaw, hasValue := item["value"]
		// Some Postman exports omit the current value or serialize it as null
		// while retaining the initial value. Use initialValue in that case.
		if !hasValue || isJSONNull(valueRaw) {
			if initialRaw, ok := item["initialValue"]; ok {
				valueRaw = initialRaw
				hasValue = true
			}
		}
		value := ""
		if hasValue {
			value, err = scalarString(valueRaw, itemPath+".value")
			if err != nil {
				return nil, err
			}
		}
		variables[key] = value
	}
	return variables, nil
}

func normalizeName(value, path string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", importError(path, "must not be empty")
	}
	if value == "." || value == ".." || strings.ContainsAny(value, "/\\") {
		return "", importError(path, "must not contain path separators or traversal components")
	}
	for _, r := range value {
		if r == 0 || unicode.IsControl(r) {
			return "", importError(path, "must not contain control characters")
		}
	}
	return value, nil
}

func validVariableName(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '_' {
			continue
		}
		return false
	}
	return true
}

func scalarString(raw json.RawMessage, path string) (string, error) {
	trimmed := bytes.TrimSpace(raw)
	if bytes.Equal(trimmed, []byte("null")) {
		return "", nil
	}
	var stringValue string
	if err := json.Unmarshal(trimmed, &stringValue); err == nil {
		return stringValue, nil
	}
	var number json.Number
	if err := json.Unmarshal(trimmed, &number); err == nil {
		return number.String(), nil
	}
	var boolValue bool
	if err := json.Unmarshal(trimmed, &boolValue); err == nil {
		return strconv.FormatBool(boolValue), nil
	}
	return "", importError(path, "must be a scalar string, number, boolean, or null")
}

func decodeRoot(data []byte) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var raw json.RawMessage
	if err := decoder.Decode(&raw); err != nil {
		return nil, importError("environment", "invalid JSON: %v", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, importError("environment", "trailing JSON data is not allowed")
		}
		return nil, importError("environment", "invalid trailing JSON data: %v", err)
	}
	return decodeObject(raw, "environment")
}

func decodeObject(raw json.RawMessage, path string) (map[string]json.RawMessage, error) {
	if len(raw) == 0 || isJSONNull(raw) {
		return nil, importError(path, "must be a JSON object")
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil, importError(path, "must be a JSON object: %v", err)
	}
	if object == nil {
		return nil, importError(path, "must be a JSON object")
	}
	return object, nil
}

func decodeArray(raw json.RawMessage, path string) ([]json.RawMessage, error) {
	if len(raw) == 0 || isJSONNull(raw) {
		return nil, importError(path, "must be a JSON array")
	}
	var array []json.RawMessage
	if err := json.Unmarshal(raw, &array); err != nil {
		return nil, importError(path, "must be a JSON array: %v", err)
	}
	if array == nil {
		return nil, importError(path, "must be a JSON array")
	}
	return array, nil
}

func requiredString(obj map[string]json.RawMessage, key, path string) (string, error) {
	raw, ok := obj[key]
	if !ok {
		return "", importError(path+"."+key, "field is required")
	}
	return decodeString(raw, path+"."+key)
}

func decodeString(raw json.RawMessage, path string) (string, error) {
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", importError(path, "must be a string: %v", err)
	}
	return value, nil
}

func optionalBool(obj map[string]json.RawMessage, key, path string) (bool, error) {
	raw, ok := obj[key]
	if !ok {
		return true, nil
	}
	var value bool
	if err := json.Unmarshal(raw, &value); err != nil {
		return false, importError(path+"."+key, "must be a boolean: %v", err)
	}
	return value, nil
}

func isJSONNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

func importError(path, format string, args ...any) error {
	return fmt.Errorf("postman environment import at %s: %s", path, fmt.Sprintf(format, args...))
}
