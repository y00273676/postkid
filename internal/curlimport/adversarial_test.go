package curlimport

import "testing"

func TestParseCopyAsCurlVariants(t *testing.T) {
	tests := []struct {
		name       string
		command    string
		wantMethod string
		wantURL    string
		wantBody   string
		wantHeader map[string]string
	}{
		{
			name: "chrome linux bash",
			command: `curl 'https://api.example.test/v1/items?search=one&limit=10' \
  -H 'accept: application/json, text/plain, */*' \
  -H 'authorization: Bearer chrome-token' \
  --compressed \
  --data-raw '{"name":"demo","enabled":true}'`,
			wantMethod: "POST",
			wantURL:    "https://api.example.test/v1/items?search=one&limit=10",
			wantBody:   `{"name":"demo","enabled":true}`,
			wantHeader: map[string]string{
				"accept":        "application/json, text/plain, */*",
				"authorization": "Bearer chrome-token",
			},
		},
		{
			name: "chrome macos quoted json",
			command: `curl 'https://api.example.test/login?from=chrome' \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Basic dXNlcjpwYXNz' \
  --data-raw '{"email":"dev@example.test","message":"it'"'"'s fine"}'`,
			wantMethod: "POST",
			wantURL:    "https://api.example.test/login?from=chrome",
			wantBody:   `{"email":"dev@example.test","message":"it's fine"}`,
			wantHeader: map[string]string{
				"Content-Type":  "application/json",
				"Authorization": "Basic dXNlcjpwYXNz",
			},
		},
		{
			name: "linux continuation and short flags",
			command: `curl -X PATCH 'https://api.example.test/v1/items/42' \
  -H 'X-Trace-Id: trace-42' \
  -d '{"state":"ready"}'`,
			wantMethod: "PATCH",
			wantURL:    "https://api.example.test/v1/items/42",
			wantBody:   `{"state":"ready"}`,
			wantHeader: map[string]string{"X-Trace-Id": "trace-42"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.command)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if got.Method != tt.wantMethod || got.URL != tt.wantURL || got.Body != tt.wantBody {
				t.Fatalf("request = %#v, want method=%q url=%q body=%q", got, tt.wantMethod, tt.wantURL, tt.wantBody)
			}
			for key, want := range tt.wantHeader {
				if got.Headers[key] != want {
					t.Errorf("header %q = %q, want %q", key, got.Headers[key], want)
				}
			}
		})
	}
}

func TestParseBearerAndBasicHeaders(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{name: "bearer", value: "Bearer eyJhbGciOiJIUzI1NiJ9"},
		{name: "basic", value: "Basic dXNlcjpwYXNz"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse("curl 'https://api.example.test/me' -H 'Authorization: " + tt.value + "'")
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if got.Headers["Authorization"] != tt.value {
				t.Fatalf("Authorization = %q, want %q", got.Headers["Authorization"], tt.value)
			}
		})
	}
}

func TestParseGetWithDataMovesDataIntoQuery(t *testing.T) {
	got, err := Parse("curl 'https://api.example.test/search?existing=yes' --get --data 'q=hello world' --data 'page=2'")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.Method != "GET" {
		t.Fatalf("method = %q, want GET", got.Method)
	}
	if got.Body != "" {
		t.Fatalf("body = %q, want empty", got.Body)
	}
	if got.URL != "https://api.example.test/search?existing=yes&q=hello+world&page=2" &&
		got.URL != "https://api.example.test/search?existing=yes&page=2&q=hello+world" {
		t.Fatalf("URL = %q, want both data fields in query", got.URL)
	}
}

func TestParseSupportsLocationDoubleQuotedEscapesAndLiteralAt(t *testing.T) {
	command := `curl --location --request POST "https://api.example.test/quoted" \
  -H "X-Quoted: a \"b\"" \
  --data-raw "{\"name\":\"a b\",\"text\":\"line\\n\"}"`
	got, err := Parse(command)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.Method != "POST" || got.URL != "https://api.example.test/quoted" {
		t.Fatalf("request target = %s %s", got.Method, got.URL)
	}
	if got.Headers["X-Quoted"] != `a "b"` {
		t.Fatalf("X-Quoted = %q", got.Headers["X-Quoted"])
	}
	if got.Body != `{"name":"a b","text":"line\n"}` {
		t.Fatalf("body = %q", got.Body)
	}

	literalAt, err := Parse(`curl 'https://api.example.test/mail' --data-raw 'email@example.com'`)
	if err != nil {
		t.Fatalf("literal @ Parse: %v", err)
	}
	if literalAt.Body != "email@example.com" {
		t.Fatalf("literal @ body = %q", literalAt.Body)
	}
}

func TestParseDuplicateHeadersUseDeterministicLastValue(t *testing.T) {
	got, err := Parse("curl 'https://api.example.test' -H 'X-Test: one' -H 'x-test: two'")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.Headers["X-Test"] != "two" {
		t.Fatalf("X-Test = %q, want last duplicate value", got.Headers["X-Test"])
	}
	if len(got.Headers) != 1 {
		t.Fatalf("headers = %#v, want case-insensitive duplicate collapsed", got.Headers)
	}
}

func TestParseRejectsShellEvaluationAndFileInputs(t *testing.T) {
	commands := []string{
		"curl $(printf 'https://api.example.test')",
		"curl `printf 'https://api.example.test'`",
		"curl 'https://api.example.test' --data @/tmp/body.json",
		"curl 'https://api.example.test' -d '@/tmp/body.json'",
		"curl 'https://api.example.test' --config /tmp/curlrc",
		"curl 'https://api.example.test' --upload-file /tmp/body.json",
		"curl 'https://api.example.test' --form 'file=@/tmp/body.json'",
	}
	for _, command := range commands {
		if _, err := Parse(command); err == nil {
			t.Errorf("Parse(%q) accepted unsafe or unsupported input", command)
		}
	}
}

func TestParseRejectsMalformedQuotesAndCommandOperators(t *testing.T) {
	commands := []string{
		"curl 'https://api.example.test",
		"curl https://api.example.test -H 'X-Test: broken",
		"curl 'https://api.example.test' && curl 'https://evil.example.test'",
		"curl 'https://api.example.test'; echo compromised",
		"curl 'https://api.example.test' | sh",
	}
	for _, command := range commands {
		if _, err := Parse(command); err == nil {
			t.Errorf("Parse(%q) accepted malformed/operator input", command)
		}
	}
}

func TestParseRejectsUnsupportedFlags(t *testing.T) {
	for _, flag := range []string{"--output", "--config", "--upload-file", "--form", "--cookie-jar", "--resolve", "--proxy", "--cert", "--key"} {
		command := "curl 'https://api.example.test' " + flag + " value"
		if _, err := Parse(command); err == nil {
			t.Errorf("Parse(%q) accepted unsupported flag", command)
		}
	}
}

func TestParseRejectsEmptyOrNonCurlInput(t *testing.T) {
	for _, command := range []string{"", "   ", "wget https://api.example.test", "curl", "curl echo https://api.example.test"} {
		if _, err := Parse(command); err == nil {
			t.Errorf("Parse(%q) unexpectedly succeeded", command)
		}
	}
}

func TestParseRejectsCommandSubstitutionInArguments(t *testing.T) {
	for _, command := range []string{
		`curl "$(printf https://api.example.test)"`,
		"curl 'https://api.example.test' --data-raw '$(touch /tmp/pwned)'",
	} {
		if _, err := Parse(command); err == nil {
			t.Errorf("Parse(%q) accepted command substitution", command)
		}
	}
}
