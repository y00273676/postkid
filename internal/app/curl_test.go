package app

import (
	"strings"
	"testing"

	"go.planetmeican.com/yangguang/postkid/internal/model"
)

func TestExportCurl_GET(t *testing.T) {
	r := model.ResolvedRequest{
		Method: "GET",
		URL:    "https://api.example.com/orders?detail=true",
		Headers: map[string]string{
			"Accept": "application/json",
		},
	}
	out := ExportCurl(r)
	if !strings.HasPrefix(out, "curl") {
		t.Errorf("should start with curl: %s", out)
	}
	if strings.Contains(out, "-X GET") {
		t.Errorf("GET should not include -X GET: %s", out)
	}
	if !strings.Contains(out, "https://api.example.com/orders?detail=true") {
		t.Errorf("missing URL: %s", out)
	}
	if !strings.Contains(out, "Accept: application/json") {
		t.Errorf("missing header: %s", out)
	}
}

func TestExportCurl_POST(t *testing.T) {
	r := model.ResolvedRequest{
		Method: "POST",
		URL:    "https://api.example.com/orders",
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
		Body: `{"sku":"A-1","qty":2}`,
	}
	out := ExportCurl(r)
	if !strings.Contains(out, "-X POST") {
		t.Errorf("POST should include -X POST: %s", out)
	}
	if !strings.Contains(out, "-d '{\"sku\":\"A-1\",\"qty\":2}'") {
		t.Errorf("missing body: %s", out)
	}
}

func TestExportCurl_AuthHeader(t *testing.T) {
	r := model.ResolvedRequest{
		Method: "GET",
		URL:    "https://api.example.com/me",
		Headers: map[string]string{
			"Authorization": "Bearer tok-123",
		},
	}
	out := ExportCurl(r)
	if !strings.Contains(out, "Bearer tok-123") {
		t.Errorf("missing auth header: %s", out)
	}
}

func TestExportCurl_EscapeShell(t *testing.T) {
	r := model.ResolvedRequest{
		Method: "GET",
		URL:    "https://x.com/path?q=it's",
	}
	out := ExportCurl(r)
	if !strings.Contains(out, "it'\\''s") {
		t.Errorf("single quote not escaped: %s", out)
	}
}

func TestExportCurl_PUT(t *testing.T) {
	r := model.ResolvedRequest{
		Method: "PUT",
		URL:    "https://api.example.com/orders/1",
		Body:   `{"status":"shipped"}`,
	}
	out := ExportCurl(r)
	if !strings.Contains(out, "-X PUT") {
		t.Errorf("PUT should include -X PUT: %s", out)
	}
}

func TestExportCurl_DELETE(t *testing.T) {
	r := model.ResolvedRequest{
		Method: "DELETE",
		URL:    "https://api.example.com/orders/1",
	}
	out := ExportCurl(r)
	if !strings.Contains(out, "-X DELETE") {
		t.Errorf("DELETE should include -X DELETE: %s", out)
	}
}