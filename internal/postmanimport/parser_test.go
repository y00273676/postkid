package postmanimport

import (
	"mime"
	"mime/multipart"
	"strings"
	"testing"

	"go.planetmeican.com/yangguang/postkid/internal/model"
)

const schemaV21 = "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"

func TestParseCollection(t *testing.T) {
	data := `{
  "info": {"name": "Payments", "schema": "` + schemaV21 + `"},
  "variable": [
    {"key": "base", "value": "https://api.example.test"},
    {"key": "count", "value": 7},
    {"key": "disabled", "value": "hidden", "disabled": true}
  ],
  "item": [{
    "name": "Users",
    "item": [{
      "name": "List",
      "request": {
        "method": "get",
        "header": [
          {"key": "Accept", "value": "application/json"},
          {"key": "X-Disabled", "value": "ignored", "disabled": true}
        ],
        "url": {
          "raw": "{{base}}/users",
          "query": [
            {"key": "page", "value": "1"},
            {"key": "draft", "value": "true", "disabled": true}
          ]
        },
        "body": {"mode": "raw", "raw": "{\"active\":true}"}
      }
    }]
  }]}
`

	result, err := Parse([]byte(data))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if result.Collection.Name != "Payments" || result.Imported != 1 {
		t.Fatalf("result = %#v, want Payments with one request", result)
	}
	if got := result.Collection.Variables; got["base"] != "https://api.example.test" || got["count"] != "7" {
		t.Fatalf("variables = %#v", got)
	}
	if _, ok := result.Collection.Variables["disabled"]; ok {
		t.Fatal("disabled collection variable was imported")
	}
	req := result.Collection.Requests[0]
	if req.Name != "Users › List" || req.Method != "GET" || req.URL != "{{base}}/users" {
		t.Fatalf("request identity = %#v", req)
	}
	if req.Headers["Accept"] != "application/json" || len(req.Headers) != 1 {
		t.Fatalf("headers = %#v", req.Headers)
	}
	if req.Params["page"] != "1" || len(req.Params) != 1 {
		t.Fatalf("params = %#v", req.Params)
	}
	if req.Body != `{"active":true}` {
		t.Fatalf("body = %q", req.Body)
	}
}

func TestParseURLObjectWithoutRaw(t *testing.T) {
	data := collectionJSON(`{
    "name":"Get",
    "request":{
      "method":"GET",
      "url":{
        "protocol":"https",
        "host":["api", "example", "test"],
        "path":["v1", "users"],
        "query":[{"key":"q","value":"{{term}}"}]
      }
    }
  }`)
	result, err := Parse([]byte(data))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	req := result.Collection.Requests[0]
	if req.URL != "https://api.example.test/v1/users" {
		t.Fatalf("URL = %q", req.URL)
	}
	if req.Params["q"] != "{{term}}" {
		t.Fatalf("params = %#v", req.Params)
	}
}

func TestParseBodyModes(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "raw", body: `{"mode":"raw","raw":"hello {{name}}"}`, want: "hello {{name}}"},
		{name: "urlencoded", body: `{"mode":"urlencoded","urlencoded":[{"key":"a","value":"1"},{"key":"skip","value":"x","disabled":true},{"key":"b key","value":"two words"}]}`, want: "a=1&b+key=two+words"},
		{name: "formdata", body: `{"mode":"formdata","formdata":[{"key":"a","value":"1"},{"key":"b","value":"two"}]}`, want: "a=1&b=two"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := collectionJSON(`{"name":"` + tt.name + `","request":{"method":"POST","url":"https://example.test","body":` + tt.body + `}}`)
			result, err := Parse([]byte(data))
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			req := result.Collection.Requests[0]
			if tt.name == "formdata" {
				mediaType, params, err := mime.ParseMediaType(req.Headers["Content-Type"])
				if err != nil || mediaType != "multipart/form-data" || params["boundary"] == "" {
					t.Fatalf("multipart content type = %q, err=%v", req.Headers["Content-Type"], err)
				}
				reader := multipart.NewReader(strings.NewReader(req.Body), params["boundary"])
				form, err := reader.ReadForm(1024)
				if err != nil || strings.Join(form.Value["a"], "") != "1" || strings.Join(form.Value["b"], "") != "two" {
					t.Fatalf("multipart form = %#v, err=%v", form, err)
				}
				return
			}
			if got := req.Body; got != tt.want {
				t.Fatalf("body = %q, want %q", got, tt.want)
			}
			if tt.name == "urlencoded" && req.Headers["Content-Type"] != "application/x-www-form-urlencoded" {
				t.Fatalf("urlencoded content type = %#v", req.Headers)
			}
		})
	}
}

func TestParseRawQueryIsNotDuplicatedAndNullFieldsInherit(t *testing.T) {
	data := `{"info":{"name":"Imported","schema":"SCHEMA"},"auth":{"type":"bearer","bearer":[{"key":"token","value":"parent"}]},"item":[{"name":"Get /users","request":{"method":"GET","auth":null,"body":null,"url":{"raw":"https://example.test/users?enabled=1&disabled=2#section","query":[{"key":"enabled","value":"1"},{"key":"disabled","value":"2","disabled":true}]}}}]}`
	data = strings.Replace(data, "SCHEMA", schemaV21, 1)
	result, err := Parse([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	req := result.Collection.Requests[0]
	if req.URL != "https://example.test/users#section" || req.Params["enabled"] != "1" || len(req.Params) != 1 {
		t.Fatalf("request target = %#v", req)
	}
	if req.AuthType != model.AuthBearer || req.AuthToken != "parent" {
		t.Fatalf("inherited auth = %#v", req)
	}
	if strings.ContainsAny(req.Name, "/\\") {
		t.Fatalf("unsafe imported name = %q", req.Name)
	}
}

func TestParseDisabledFilePartIsIgnoredAndMultipartHeaderGetsBoundary(t *testing.T) {
	data := collectionJSON(`{"name":"Upload","request":{"method":"POST","url":"https://example.test","header":[{"key":"content-type","value":"multipart/form-data"}],"body":{"mode":"formdata","formdata":[{"key":"ignored","type":"file","src":"/tmp/no-read","disabled":true},{"key":"name","value":"postkid"}]}}}`)
	result, err := Parse([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	req := result.Collection.Requests[0]
	if !strings.Contains(req.Headers["content-type"], "boundary=") || strings.Contains(req.Body, "ignored") {
		t.Fatalf("multipart request = %#v", req)
	}
}

func TestParseAuthInheritanceAndOverride(t *testing.T) {
	data := `{"info":{"name":"Imported","schema":"` + schemaV21 + `"},
  "auth":{"type":"bearer","bearer":[{"key":"token","value":"{{collectionToken}}"}]},
  "item":[
    {"name":"Folder","auth":{"type":"basic","basic":[{"key":"username","value":"alice"},{"key":"password","value":"secret"}]},"item":[
      {"name":"Inherited","request":{"method":"GET","url":"https://example.test/inherited"}},
      {"name":"NoAuth","request":{"method":"GET","url":"https://example.test/noauth","auth":{"type":"noauth"}}},
      {"name":"Bearer","request":{"method":"GET","url":"https://example.test/bearer","auth":{"type":"bearer","bearer":[{"key":"token","value":"request-token"}]}}}
    ]},
    {"name":"CollectionAuth","request":{"method":"GET","url":"https://example.test/collection"}}
  ]
}`
	result, err := Parse([]byte(data))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	byName := make(map[string]model.Request)
	for _, req := range result.Collection.Requests {
		byName[req.Name] = req
	}
	if got := byName["Folder › Inherited"]; got.AuthType != model.AuthBasic || got.AuthUsername != "alice" || got.AuthPassword != "secret" {
		t.Fatalf("inherited auth = %#v", got)
	}
	if got := byName["Folder › NoAuth"]; got.AuthType != model.AuthNone {
		t.Fatalf("noauth override = %#v", got)
	}
	if got := byName["Folder › Bearer"]; got.AuthType != model.AuthBearer || got.AuthToken != "request-token" {
		t.Fatalf("request auth = %#v", got)
	}
	if got := byName["CollectionAuth"]; got.AuthType != model.AuthBearer || got.AuthToken != "{{collectionToken}}" {
		t.Fatalf("collection auth = %#v", got)
	}
}

func TestParseRejectsUnsupportedOrAmbiguousInput(t *testing.T) {
	tests := []struct {
		name string
		data string
		want string
	}{
		{name: "trailing", data: collectionJSON(`{"name":"x","request":{"method":"GET","url":"https://example.test"}}`) + ` {}`, want: "trailing"},
		{name: "schema", data: strings.Replace(collectionJSON(`{"name":"x","request":{"method":"GET","url":"https://example.test"}}`), schemaV21, "https://schema.getpostman.com/json/collection/v2.0.0/collection.json", 1), want: "schema"},
		{name: "fake schema patch", data: strings.Replace(collectionJSON(`{"name":"x","request":{"method":"GET","url":"https://example.test"}}`), schemaV21, "https://schema.getpostman.com/json/collection/v2.1.evil/collection.json", 1), want: "schema"},
		{name: "unsupported variable key", data: `{"info":{"name":"x","schema":"` + schemaV21 + `"},"variable":[{"key":"bad-key","value":"x"}],"item":[]}`, want: "variable name"},
		{name: "header injection", data: collectionJSON(`{"name":"x","request":{"method":"GET","url":"https://example.test","header":[{"key":"X-Test","value":"safe\r\nInjected: true"}]}}`), want: "newline"},
		{name: "method", data: collectionJSON(`{"name":"x","request":{"method":"HEAD","url":"https://example.test"}}`), want: "unsupported HTTP method"},
		{name: "file", data: collectionJSON(`{"name":"x","request":{"method":"POST","url":"https://example.test","body":{"mode":"formdata","formdata":[{"key":"file","type":"file","src":"/tmp/a"}]}}}`), want: "file form item"},
		{name: "auth", data: collectionJSON(`{"name":"x","request":{"method":"GET","url":"https://example.test","auth":{"type":"oauth2"}}}`), want: "unsupported auth type"},
		{name: "duplicate", data: `{"info":{"name":"x","schema":"` + schemaV21 + `"},"item":[{"name":"A","request":{"method":"GET","url":"https://example.test"}},{"name":"A","request":{"method":"GET","url":"https://example.test/2"}}]}`, want: "duplicate request name"},
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

func TestParseErrorContainsRequestPath(t *testing.T) {
	data := collectionJSON(`{"name":"Bad Request","request":{"method":"HEAD","url":"https://example.test"}}`)
	_, err := Parse([]byte(data))
	if err == nil || !strings.Contains(err.Error(), "Bad Request") {
		t.Fatalf("error = %v, want request path", err)
	}
}

func collectionJSON(item string) string {
	return `{"info":{"name":"Imported","schema":"` + schemaV21 + `"},"item":[` + item + `]}`
}
