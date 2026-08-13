package curlimport

import (
	"errors"
	"strings"
	"testing"

	"go.planetmeican.com/yangguang/postkid/internal/model"
)

func TestParseTable(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		method  string
		url     string
		body    string
		headers map[string]string
		auth    []string
	}{
		{
			name:   "simple get",
			input:  "curl https://example.test/health",
			method: "GET",
			url:    "https://example.test/health",
		},
		{
			name:   "chrome post",
			input:  "curl 'https://example.test/api' -H 'accept: application/json' -H 'content-type: application/json' --data-raw '{\"ok\":true}' --compressed",
			method: "POST",
			url:    "https://example.test/api",
			body:   `{"ok":true}`,
			headers: map[string]string{
				"accept":       "application/json",
				"content-type": "application/json",
			},
		},
		{
			name:   "explicit method and url option",
			input:  "curl --request PATCH --url 'https://example.test/items/1'",
			method: "PATCH",
			url:    "https://example.test/items/1",
		},
		{
			name:   "compact short options",
			input:  "curl -XPUT -HContent-Type:\\ application/json -d'hello' https://example.test",
			method: "PUT",
			url:    "https://example.test",
			body:   "hello",
			headers: map[string]string{
				"Content-Type": "application/json",
			},
		},
		{
			name:   "basic auth",
			input:  "curl -u 'alice:p@ss:word' https://example.test/me",
			method: "GET",
			url:    "https://example.test/me",
			auth:   []string{model.AuthBasic, "alice", "p@ss:word"},
		},
		{
			name:   "duplicate headers last wins",
			input:  "curl -H 'X-Trace: first' -H 'x-trace: second' https://example.test",
			method: "GET",
			url:    "https://example.test",
			headers: map[string]string{
				"X-Trace": "second",
			},
		},
		{
			name:   "multiple data values",
			input:  "curl https://example.test -d 'a=1' --data-raw 'b=2' --data-binary 'c=3'",
			method: "POST",
			url:    "https://example.test",
			body:   "a=1&b=2&c=3",
		},
		{
			name:   "url encode data",
			input:  "curl https://example.test -d 'name=alice smith' --data-urlencode 'q=a b'",
			method: "POST",
			url:    "https://example.test",
			body:   "name=alice smith&q=a+b",
		},
		{
			name:   "get moves data to query",
			input:  "curl --get 'https://example.test/search?lang=en' --data 'q=hello world' --data-urlencode 'page=1 2'",
			method: "GET",
			url:    "https://example.test/search?lang=en&q=hello+world&page=1+2",
		},
		{
			name:   "double quote escapes",
			input:  `curl "https://example.test/path?q=hello\ world" -H "X-Name: \"value\""`,
			method: "GET",
			url:    `https://example.test/path?q=hello\ world`,
			headers: map[string]string{
				"X-Name": `"value"`,
			},
		},
		{
			name:   "exported shell apostrophe escape",
			input:  "curl 'https://example.test' -d 'it'\\''s'",
			method: "POST",
			url:    "https://example.test",
			body:   "it's",
		},
		{
			name: "line continuation",
			input: `curl https://example.test \
  -H 'Accept: application/json' \
  -X POST`,
			method: "POST",
			url:    "https://example.test",
			headers: map[string]string{
				"Accept": "application/json",
			},
		},
		{
			name:   "safe clustered flags",
			input:  "curl -sSvi https://example.test",
			method: "GET",
			url:    "https://example.test",
		},
		{
			name:   "location option",
			input:  "curl --location --request GET https://example.test",
			method: "GET",
			url:    "https://example.test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.input)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if got.Method != tt.method || got.URL != tt.url || got.Body != tt.body {
				t.Fatalf("request = %#v, want method=%q url=%q body=%q", got, tt.method, tt.url, tt.body)
			}
			if len(got.Headers) != len(tt.headers) {
				t.Fatalf("headers = %#v, want %#v", got.Headers, tt.headers)
			}
			for key, want := range tt.headers {
				if got.Headers[key] != want {
					t.Errorf("header %q = %q, want %q", key, got.Headers[key], want)
				}
			}
			if len(tt.auth) > 0 {
				if got.AuthType != tt.auth[0] || got.AuthUsername != tt.auth[1] || got.AuthPassword != tt.auth[2] {
					t.Fatalf("auth = %#v, want %#v", got, tt.auth)
				}
			} else if got.AuthType != "" {
				t.Fatalf("unexpected auth = %#v", got)
			}
		})
	}
}

func TestParseDoubleQuotesPreserveNonSpecialBackslashes(t *testing.T) {
	got, err := Parse(`curl "https://example.test/path" --data-raw "line\nvalue\q"`)
	if err != nil {
		t.Fatal(err)
	}
	if got.Body != `line\nvalue\q` {
		t.Fatalf("body = %q, want literal backslashes", got.Body)
	}
}

func TestParseErrorTable(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		reason string
		pos    bool
	}{
		{name: "empty", input: "", reason: "command is empty"},
		{name: "not curl", input: "wget https://example.test", reason: "expected a curl command", pos: true},
		{name: "missing url", input: "curl -H 'Accept: text/plain'", reason: "a URL is required"},
		{name: "unterminated single quote", input: "curl 'https://example.test", reason: "unterminated quote"},
		{name: "unterminated double quote", input: `curl "https://example.test`, reason: "unterminated quote"},
		{name: "dangling escape", input: "curl https://example.test \\", reason: "dangling backslash"},
		{name: "backtick", input: "curl 'https://example.test/`id`'", reason: "backtick command substitution"},
		{name: "command substitution", input: "curl 'https://example.test/$(id)'", reason: "command substitution"},
		{name: "variable expansion", input: `curl "$URL"`, reason: "variable expansion"},
		{name: "pipe", input: "curl https://example.test | cat", reason: "shell operators"},
		{name: "semicolon", input: "curl https://example.test; echo bad", reason: "shell operators"},
		{name: "unknown option", input: "curl --output /tmp/x https://example.test", reason: "unsupported or unsafe"},
		{name: "unsafe data file", input: "curl https://example.test -d '@payload.json'", reason: "@file request data"},
		{name: "unsafe user file", input: "curl https://example.test -u '@credentials'", reason: "credential files"},
		{name: "bad header", input: "curl https://example.test -H 'Accept'", reason: "Name: value"},
		{name: "bad method", input: "curl https://example.test -X HEAD", reason: "unsupported HTTP method"},
		{name: "duplicate url", input: "curl https://one.test https://two.test", reason: "more than one URL"},
		{name: "bad url", input: "curl file:///tmp/payload", reason: "absolute http or https"},
		{name: "missing request value", input: "curl --request", reason: "missing request method"},
		{name: "missing header value", input: "curl -H", reason: "missing header"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(tt.input)
			if err == nil {
				t.Fatal("Parse unexpectedly succeeded")
			}
			var parseErr *ParseError
			if !errors.As(err, &parseErr) {
				t.Fatalf("error = %T %v, want *ParseError", err, err)
			}
			if !strings.Contains(parseErr.Reason, tt.reason) {
				t.Fatalf("reason = %q, want substring %q", parseErr.Reason, tt.reason)
			}
			if tt.pos && parseErr.Position <= 0 {
				t.Fatalf("position = %d, want a token position", parseErr.Position)
			}
			if !strings.Contains(err.Error(), "position") {
				t.Fatalf("error = %q, want readable position", err)
			}
		})
	}
}

func TestParseQuotedNewlineAndContinuation(t *testing.T) {
	got, err := Parse("curl https://example.test -d 'line one\nline two'")
	if err != nil {
		t.Fatal(err)
	}
	if got.Body != "line one\nline two" {
		t.Fatalf("body = %q", got.Body)
	}
	got, err = Parse("curl https://example.test " + "\\" + "\n-d foo")
	if err != nil {
		t.Fatal(err)
	}
	if got.Body != "foo" {
		t.Fatalf("continued body = %q", got.Body)
	}
}

func TestParseNoShellExecution(t *testing.T) {
	if _, err := Parse("curl https://example.test > /tmp/postkid-curl-import-test"); err == nil {
		t.Fatal("shell redirection unexpectedly accepted")
	}
}
