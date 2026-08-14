package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writePostmanEnvironment(t *testing.T, path, name string) {
	t.Helper()
	data := `{
  "id": "postkid-test-environment",
  "name": "` + name + `",
  "values": [
    {"key": "base_url", "value": "https://sandbox.example.test", "enabled": true},
    {"key": "token", "value": "disabled-token", "enabled": false},
    {"key": "count", "value": 7, "enabled": true},
    {"key": "feature", "value": true, "enabled": true},
    {"key": "empty", "enabled": true}
  ],
  "_postman_variable_scope": "environment"
}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestPostmanEnvironmentImportCommandUsage(t *testing.T) {
	m := postmanTestModel(t)
	for _, input := range []string{"import postman-env", `import postman-env ""`} {
		cmd := m.executeCommand(input)
		if cmd == nil {
			t.Fatalf("%q returned nil command", input)
		}
		result := cmd()
		msg, ok := result.(InfoMsg)
		if !ok {
			t.Fatalf("%q returned %T, want InfoMsg", input, result)
		}
		if !strings.Contains(msg.Text, "import postman-env") {
			t.Errorf("%q usage = %q", input, msg.Text)
		}
	}
}

func TestPostmanEnvironmentImportCommandSupportsQuotedSpacesAndSwitches(t *testing.T) {
	m := postmanTestModel(t)
	if err := m.app.SetEnvironment("sandbox"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "my imported environment.json")
	writePostmanEnvironment(t, path, "imported environment")

	cmd := m.executeCommand(`import postman-env "` + path + `"`)
	if cmd == nil {
		t.Fatal("import postman-env returned nil command")
	}
	result := cmd()
	msg, ok := result.(PostmanEnvironmentImportSavedMsg)
	if !ok {
		t.Fatalf("import result = %T, want PostmanEnvironmentImportSavedMsg", result)
	}
	if msg.Environment != "imported environment" || msg.Imported != 4 {
		t.Fatalf("import result = %#v", msg)
	}

	updated, _ := m.Update(msg)
	m = updated.(Model)
	if current := m.app.CurrentEnvironment(); current == nil || current.Name != "imported environment" {
		t.Fatalf("current environment = %#v, want imported environment", current)
	}
	if m.app.CurrentEnvironment().Variables["base_url"] != "https://sandbox.example.test" {
		t.Fatalf("variables = %#v", m.app.CurrentEnvironment().Variables)
	}
	if _, ok := m.app.CurrentEnvironment().Variables["token"]; ok {
		t.Fatal("disabled variable was imported")
	}
	if m.app.CurrentEnvironment().Variables["count"] != "7" || m.app.CurrentEnvironment().Variables["feature"] != "true" {
		t.Fatalf("scalar variables = %#v", m.app.CurrentEnvironment().Variables)
	}
	if m.app.CurrentEnvironment().Variables["empty"] != "" {
		t.Fatalf("empty variable = %#v", m.app.CurrentEnvironment().Variables)
	}
	if m.app.CurrentEnvironment().Name != m.app.Environments()[len(m.app.Environments())-1].Name {
		t.Fatalf("environment cache = %#v", m.app.Environments())
	}
	if !strings.Contains(m.statusMsg, "imported environment") || !strings.Contains(m.statusMsg, "4 variables") {
		t.Fatalf("status = %q", m.statusMsg)
	}
}

func TestPostmanEnvironmentImportFailureKeepsSelectionAndCache(t *testing.T) {
	m := postmanTestModel(t)
	if err := m.app.SetEnvironment("sandbox"); err != nil {
		t.Fatal(err)
	}
	wantName := m.app.CurrentEnvironment().Name
	wantCount := len(m.app.Environments())
	path := filepath.Join(t.TempDir(), "malformed environment.json")
	if err := os.WriteFile(path, []byte(`{"name":`), 0o600); err != nil {
		t.Fatal(err)
	}

	result := m.executeCommand(`import postman-env "` + path + `"`)()
	msg, ok := result.(PostmanEnvironmentImportSaveFailedMsg)
	if !ok {
		t.Fatalf("import result = %T, want PostmanEnvironmentImportSaveFailedMsg", result)
	}
	if msg.Err == nil || !strings.Contains(msg.Err.Error(), "parse Postman environment") {
		t.Fatalf("error = %v", msg.Err)
	}
	updated, _ := m.Update(msg)
	m = updated.(Model)
	if current := m.app.CurrentEnvironment(); current == nil || current.Name != wantName {
		t.Fatalf("selection changed after malformed import: %#v", current)
	}
	if len(m.app.Environments()) != wantCount {
		t.Fatalf("environment count changed after malformed import: %d", len(m.app.Environments()))
	}
	if m.err == nil {
		t.Fatal("expected import error in model")
	}
}

func TestPostmanEnvironmentImportDuplicateKeepsSelection(t *testing.T) {
	m := postmanTestModel(t)
	if err := m.app.SetEnvironment("sandbox"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "duplicate.json")
	writePostmanEnvironment(t, path, "prod")

	result := m.executeCommand("import postman-env " + path)()
	msg, ok := result.(PostmanEnvironmentImportSaveFailedMsg)
	if !ok {
		t.Fatalf("import result = %T, want PostmanEnvironmentImportSaveFailedMsg", result)
	}
	if msg.Err == nil || !strings.Contains(msg.Err.Error(), "already exists") {
		t.Fatalf("error = %v", msg.Err)
	}
	if current := m.app.CurrentEnvironment(); current == nil || current.Name != "sandbox" {
		t.Fatalf("selection changed after duplicate import: %#v", current)
	}
}

func TestReadPostmanEnvironmentFileRejectsNonRegularAndOversizedFiles(t *testing.T) {
	if _, err := readPostmanEnvironmentFile(t.TempDir()); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("directory error = %v", err)
	}
	path := filepath.Join(t.TempDir(), "large.json")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxPostmanEnvironmentImportBytes + 1); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := readPostmanEnvironmentFile(path); err == nil || !strings.Contains(err.Error(), "64 MiB") {
		t.Fatalf("oversize error = %v", err)
	}
}

func TestParsePostmanEnvironmentRejectsDuplicateAndInvalidValues(t *testing.T) {
	tests := []struct {
		name string
		data string
		want string
	}{
		{
			name: "missing values",
			data: `{"name":"env"}`,
			want: "values",
		},
		{
			name: "duplicate",
			data: `{"name":"env","values":[{"key":"token","value":"a"},{"key":"token","value":"b"}]}`,
			want: "duplicate",
		},
		{
			name: "composite",
			data: `{"name":"env","values":[{"key":"token","value":{"secret":"x"}}]}`,
			want: "scalar",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parsePostmanEnvironment([]byte(tt.data))
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), tt.want) {
				t.Fatalf("parse error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestPostmanEnvironmentImportRequiresApplication(t *testing.T) {
	var m Model
	cmd := m.importPostmanEnvironmentPath("/tmp/environment.json")
	if cmd == nil {
		t.Fatal("import command = nil")
	}
	msg, ok := cmd().(PostmanEnvironmentImportSaveFailedMsg)
	if !ok || msg.Err == nil || !strings.Contains(msg.Err.Error(), "application unavailable") {
		t.Fatalf("message = %#v", msg)
	}
}
