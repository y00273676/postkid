package postmanenv

import (
	"strings"
	"testing"
)

func TestParseEnvironment(t *testing.T) {
	data := `{
  "id": "env-id",
  "name": " Payments ",
  "values": [
    {"key":"base_url","value":"https://api.example.test","enabled":true},
    {"key":"count","value":7},
    {"key":"enabled_bool","value":true},
    {"key":"initial_only","initialValue":"from-initial"},
    {"key":"null_value","value":null,"initialValue":"fallback"},
    {"key":"disabled","value":"ignored","enabled":false}
  ],
  "_postman_variable_scope": "environment"
}`

	result, err := Parse([]byte(data))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if result.Environment.Name != "Payments" || result.Imported != 5 {
		t.Fatalf("result = %#v, want Payments with five variables", result)
	}
	want := map[string]string{
		"base_url":     "https://api.example.test",
		"count":        "7",
		"enabled_bool": "true",
		"initial_only": "from-initial",
		"null_value":   "fallback",
	}
	for key, expected := range want {
		if got := result.Environment.Variables[key]; got != expected {
			t.Errorf("variables[%q] = %q, want %q", key, got, expected)
		}
	}
	if _, ok := result.Environment.Variables["disabled"]; ok {
		t.Fatal("disabled variable was imported")
	}
}

func TestParseEnvironmentAlias(t *testing.T) {
	result, err := ParseEnvironment([]byte(`{"name":"local","values":[]}`))
	if err != nil || result.Environment.Name != "local" {
		t.Fatalf("ParseEnvironment() = %#v, %v", result, err)
	}
}

func TestParseEnvironmentRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name string
		data string
		want string
	}{
		{name: "malformed", data: `{`, want: "invalid JSON"},
		{name: "trailing", data: `{"name":"x","values":[]} {}`, want: "trailing"},
		{name: "root array", data: `[]`, want: "JSON object"},
		{name: "missing name", data: `{"values":[]}`, want: "name"},
		{name: "missing values", data: `{"name":"x"}`, want: "values"},
		{name: "values object", data: `{"name":"x","values":{}}`, want: "JSON array"},
		{name: "invalid name", data: `{"name":"../x","values":[]}`, want: "path separators"},
		{name: "invalid key", data: `{"name":"x","values":[{"key":"bad-key","value":"x"}]}`, want: "variable name"},
		{name: "duplicate key", data: `{"name":"x","values":[{"key":"token","value":"one"},{"key":" token ","value":"two"}]}`, want: "duplicate"},
		{name: "value object", data: `{"name":"x","values":[{"key":"token","value":{}}]}`, want: "scalar"},
		{name: "enabled type", data: `{"name":"x","values":[{"key":"token","value":"x","enabled":"yes"}]}`, want: "boolean"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.data))
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tt.want)) {
				t.Fatalf("Parse() error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestParseEnvironmentDisabledEntriesAreIgnoredBeforeValidation(t *testing.T) {
	data := `{"name":"x","values":[{"key":"not a valid key","enabled":false},{"value":{},"enabled":false}]}`
	result, err := Parse([]byte(data))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if result.Imported != 0 || len(result.Environment.Variables) != 0 {
		t.Fatalf("disabled entries imported: %#v", result)
	}
}
