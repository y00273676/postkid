package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.planetmeican.com/yangguang/postkid/internal/app"
)

func writePostmanCollection(t *testing.T, path, name string) {
	t.Helper()
	data := `{
  "info": {
    "name": "` + name + `",
    "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"
  },
  "item": [
    {
      "name": "get imported",
      "request": {
        "method": "GET",
        "url": "https://example.com/imported"
      }
    },
    {
      "name": "post imported",
      "request": {
        "method": "POST",
        "header": [{"key": "Content-Type", "value": "application/json"}],
        "body": {"mode": "raw", "raw": "{\"ok\":true}"},
        "url": "https://example.com/imported"
      }
    }
  ]
}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

func postmanTestModel(t *testing.T) Model {
	t.Helper()
	cfg := copyTestData(t)
	a, err := app.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return New(a)
}

func TestPostmanImportCommandUsage(t *testing.T) {
	m := postmanTestModel(t)
	for _, input := range []string{"import", "import postman", `import postman ""`} {
		cmd := m.executeCommand(input)
		if cmd == nil {
			t.Fatalf("%q returned nil command", input)
		}
		result := cmd()
		msg, ok := result.(InfoMsg)
		if !ok {
			t.Fatalf("%q returned %T, want InfoMsg", input, result)
		}
		if !strings.Contains(msg.Text, "import postman") {
			t.Errorf("%q usage = %q", input, msg.Text)
		}
	}
}

func TestTrimImportPathQuotes(t *testing.T) {
	tests := map[string]string{
		`'/tmp/my collection.json'`: "/tmp/my collection.json",
		`"/tmp/my collection.json"`: "/tmp/my collection.json",
		"/tmp/my collection.json":   "/tmp/my collection.json",
	}
	for input, want := range tests {
		if got := trimImportPathQuotes(input); got != want {
			t.Errorf("trimImportPathQuotes(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestPostmanPathArgumentPreservesRepeatedSpaces(t *testing.T) {
	input := `import postman "/tmp/my  collection.json"`
	if got := trimImportPathQuotes(postmanPathArgument(input)); got != "/tmp/my  collection.json" {
		t.Fatalf("path = %q", got)
	}
}

func TestPostmanImportCommandSupportsQuotedSpaces(t *testing.T) {
	m := postmanTestModel(t)
	path := filepath.Join(t.TempDir(), "my imported collection.json")
	writePostmanCollection(t, path, "imported-space")

	cmd := m.executeCommand(`import postman "` + path + `"`)
	if cmd == nil {
		t.Fatal("import postman returned nil command")
	}
	result := cmd()
	msg, ok := result.(PostmanImportSavedMsg)
	if !ok {
		t.Fatalf("import result = %T, want PostmanImportSavedMsg", result)
	}
	if msg.Collection != "imported-space" || msg.Imported != 2 {
		t.Fatalf("import result = %#v", msg)
	}

	updated, _ := m.Update(msg)
	m = updated.(Model)
	if !strings.Contains(m.statusMsg, "imported-space") || !strings.Contains(m.statusMsg, "2 requests") {
		t.Fatalf("status = %q", m.statusMsg)
	}
	if got := len(m.app.Collections()); got != 2 {
		t.Fatalf("collection count = %d, want 2", got)
	}
	if got := len(m.list.Items()); got != 4 {
		t.Fatalf("list item count = %d, want 4", got)
	}
	if m.curColl == nil || m.curColl.Name != "imported-space" {
		t.Fatalf("selected collection = %#v, want imported-space", m.curColl)
	}
}

func TestPostmanImportMalformedJSONKeepsSelection(t *testing.T) {
	m := postmanTestModel(t)
	m.selectCurrent()
	wantName := m.curReq.Name
	path := filepath.Join(t.TempDir(), "malformed.json")
	if err := os.WriteFile(path, []byte(`{"info":`), 0o600); err != nil {
		t.Fatal(err)
	}

	result := m.executeCommand("import postman " + path)()
	msg, ok := result.(PostmanImportSaveFailedMsg)
	if !ok {
		t.Fatalf("import result = %T, want PostmanImportSaveFailedMsg", result)
	}
	updated, _ := m.Update(msg)
	m = updated.(Model)
	if msg.Err == nil || !strings.Contains(msg.Err.Error(), "parse Postman") {
		t.Fatalf("error = %v", msg.Err)
	}
	if m.curReq == nil || m.curReq.Name != wantName {
		t.Fatalf("selection changed after malformed import: %#v", m.curReq)
	}
	if len(m.app.Collections()) != 1 {
		t.Fatalf("collection count changed after malformed import")
	}
}

func TestPostmanImportDuplicateCollectionFails(t *testing.T) {
	m := postmanTestModel(t)
	path := filepath.Join(t.TempDir(), "duplicate.json")
	writePostmanCollection(t, path, "order")

	result := m.executeCommand("import postman " + path)()
	msg, ok := result.(PostmanImportSaveFailedMsg)
	if !ok {
		t.Fatalf("import result = %T, want PostmanImportSaveFailedMsg", result)
	}
	if msg.Err == nil || !strings.Contains(msg.Err.Error(), "already exists") {
		t.Fatalf("error = %v", msg.Err)
	}
	if len(m.app.Collections()) != 1 {
		t.Fatalf("collection count changed after duplicate import")
	}
}

func TestReadPostmanCollectionFileRejectsNonRegularAndOversizedFiles(t *testing.T) {
	if _, err := readPostmanCollectionFile(t.TempDir()); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("directory error = %v", err)
	}
	path := filepath.Join(t.TempDir(), "large.json")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxPostmanImportBytes + 1); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := readPostmanCollectionFile(path); err == nil || !strings.Contains(err.Error(), "64 MiB") {
		t.Fatalf("oversize error = %v", err)
	}
}
