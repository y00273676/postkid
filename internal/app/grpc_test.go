package app

import (
	"path/filepath"
	"strings"
	"testing"

	"go.planetmeican.com/yangguang/postkid/internal/config"
	"go.planetmeican.com/yangguang/postkid/internal/model"
)

func TestResolveGRPCRequestUsesVariablePrecedenceAndMetadata(t *testing.T) {
	dir := t.TempDir()
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.CreateEnvironment("dev", nil); err != nil {
		t.Fatal(err)
	}
	if err := a.SetEnvironment("dev"); err != nil {
		t.Fatal(err)
	}
	variables := map[string]string{
		"target": "localhost:50051", "query": "environment", "token": "env-token",
	}
	if err := a.UpdateEnvironment("dev", variables); err != nil {
		t.Fatal(err)
	}

	collection := model.Collection{Variables: map[string]string{"query": "collection"}}
	req := model.Request{
		Name: "search", Protocol: model.ProtocolGRPC, URL: "{{target}}",
		Method: "grpc.testing.SearchService/Search", Body: `{"query":"{{query}}"}`,
		Headers:   map[string]string{"x-token": "{{token}}"},
		Variables: map[string]string{"query": "request"},
		GRPC:      &model.GRPCRequest{Metadata: map[string]string{"x-extra": "{{query}}"}},
	}
	resolved, err := a.ResolveGRPCRequest(req, collection)
	if err != nil {
		t.Fatalf("ResolveGRPCRequest: %v", err)
	}
	if resolved.Target != "localhost:50051" || resolved.Body != `{"query":"request"}` {
		t.Fatalf("resolved = %#v", resolved)
	}
	if resolved.Metadata["x-token"] != "env-token" || resolved.Metadata["x-extra"] != "request" {
		t.Fatalf("metadata = %#v", resolved.Metadata)
	}
}

func TestValidateRequestKeepsHTTPAndAcceptsGRPC(t *testing.T) {
	httpRequest := model.Request{Name: "http", Method: "GET", URL: "https://example.test"}
	if err := ValidateRequest(httpRequest); err != nil {
		t.Fatalf("HTTP validation: %v", err)
	}
	grpcRequest := model.Request{
		Name: "rpc", Protocol: model.ProtocolGRPC, URL: "grpc://localhost:50051",
		Method: "demo.Echo/Ping",
	}
	if err := ValidateRequest(grpcRequest); err != nil {
		t.Fatalf("gRPC validation: %v", err)
	}
	grpcRequest.GRPC = &model.GRPCRequest{ProtoFiles: []string{"echo.proto"}, DescriptorSet: "echo.protoset"}
	if err := ValidateRequest(grpcRequest); err == nil || !strings.Contains(err.Error(), "cannot be combined") {
		t.Fatalf("mixed descriptor sources error = %v", err)
	}
	grpcRequest.GRPC = &model.GRPCRequest{ImportPaths: []string{"proto"}}
	if err := ValidateRequest(grpcRequest); err == nil || !strings.Contains(err.Error(), "require proto_files") {
		t.Fatalf("orphan import paths error = %v", err)
	}
}

func TestResolveGRPCRequestNormalizesAndRejectsAmbiguousMetadata(t *testing.T) {
	a := &App{}
	base := model.Request{
		Name: "rpc", Protocol: model.ProtocolGRPC, URL: "localhost:50051", Method: "demo.Echo/Ping",
		GRPC:    &model.GRPCRequest{Metadata: map[string]string{"authorization": "explicit"}},
		Headers: map[string]string{"Authorization": "header"},
	}
	resolved, err := a.ResolveGRPCRequest(base, model.Collection{})
	if err != nil {
		t.Fatalf("ResolveGRPCRequest: %v", err)
	}
	if got := resolved.Metadata["authorization"]; got != "explicit" || len(resolved.Metadata) != 1 {
		t.Fatalf("metadata = %#v, want explicit canonical value", resolved.Metadata)
	}

	base.GRPC.Metadata = nil
	base.Headers = map[string]string{"X-Token": "one", "x-token": "two"}
	if _, err := a.ResolveGRPCRequest(base, model.Collection{}); err == nil || !strings.Contains(err.Error(), "duplicate gRPC metadata") {
		t.Fatalf("duplicate metadata error = %v", err)
	}

	base.Headers = map[string]string{"bad key": "value"}
	if _, err := a.ResolveGRPCRequest(base, model.Collection{}); err == nil || !strings.Contains(err.Error(), "invalid gRPC metadata") {
		t.Fatalf("invalid metadata error = %v", err)
	}
}

func TestResolveGRPCRequestResolvesDescriptorPathsRelativeToCollection(t *testing.T) {
	base := filepath.Join(t.TempDir(), "collections", "nested")
	collection := model.Collection{
		Name:     "orders",
		FilePath: filepath.Join(base, "orders.yaml"),
		Variables: map[string]string{
			"proto": "proto/order.proto",
			"root":  "../shared",
		},
	}
	req := model.Request{
		Name:     "get",
		Protocol: model.ProtocolGRPC,
		URL:      "localhost:50051",
		Method:   "orders.OrderService/Get",
		GRPC: &model.GRPCRequest{
			ProtoFiles:  []string{"{{proto}}", "./proto/../proto/common.proto"},
			ImportPaths: []string{"{{root}}"},
		},
	}

	resolved, err := (&App{}).ResolveGRPCRequest(req, collection)
	if err != nil {
		t.Fatalf("ResolveGRPCRequest: %v", err)
	}
	wantProto := []string{
		filepath.Join(base, "proto", "order.proto"),
		filepath.Join(base, "proto", "common.proto"),
	}
	wantImport := []string{filepath.Join(filepath.Dir(base), "shared")}
	if len(resolved.ProtoFiles) != len(wantProto) || resolved.ProtoFiles[0] != wantProto[0] || resolved.ProtoFiles[1] != wantProto[1] {
		t.Fatalf("proto files = %#v, want %#v", resolved.ProtoFiles, wantProto)
	}
	if len(resolved.ImportPaths) != 1 || resolved.ImportPaths[0] != wantImport[0] {
		t.Fatalf("import paths = %#v, want %#v", resolved.ImportPaths, wantImport)
	}
	if got := GRPCDescriptorSource(resolved); !strings.HasPrefix(got, "proto files ") {
		t.Fatalf("descriptor source = %q", got)
	}
}

func TestResolveGRPCRequestResolvesDescriptorSetAndRejectsMixedSources(t *testing.T) {
	base := filepath.Join(t.TempDir(), "collections")
	collection := model.Collection{FilePath: filepath.Join(base, "demo.yaml")}
	request := model.Request{
		Name: "get", Protocol: model.ProtocolGRPC, URL: "localhost:50051", Method: "demo.Service/Get",
		GRPC: &model.GRPCRequest{DescriptorSet: "descriptors/demo.protoset"},
	}
	resolved, err := (&App{}).ResolveGRPCRequest(request, collection)
	if err != nil {
		t.Fatalf("ResolveGRPCRequest: %v", err)
	}
	want := filepath.Join(base, "descriptors", "demo.protoset")
	if resolved.DescriptorSet != want || GRPCDescriptorSource(resolved) != "descriptor set "+want {
		t.Fatalf("descriptor set = %q, source = %q; want %q", resolved.DescriptorSet, GRPCDescriptorSource(resolved), want)
	}

	request.GRPC.ProtoFiles = []string{"service.proto"}
	if _, err := (&App{}).ResolveGRPCRequest(request, collection); err == nil || !strings.Contains(err.Error(), "cannot be combined") {
		t.Fatalf("mixed descriptor sources error = %v", err)
	}
	request.GRPC = &model.GRPCRequest{ImportPaths: []string{"proto"}}
	if _, err := (&App{}).ResolveGRPCRequest(request, collection); err == nil || !strings.Contains(err.Error(), "require proto_files") {
		t.Fatalf("orphan import paths error = %v", err)
	}
}

func TestCloneRequestDeepCopiesGRPCDescriptorConfig(t *testing.T) {
	request := model.Request{GRPC: &model.GRPCRequest{
		Metadata:    map[string]string{"x-token": "secret"},
		ProtoFiles:  []string{"service.proto"},
		ImportPaths: []string{"proto"},
		TLS:         &model.GRPCTLSConfig{CAFile: "ca.pem"},
	}}
	cloned := cloneRequest(request)
	cloned.GRPC.Metadata["x-token"] = "changed"
	cloned.GRPC.ProtoFiles[0] = "other.proto"
	cloned.GRPC.ImportPaths[0] = "other"
	cloned.GRPC.TLS.CAFile = "other.pem"
	if request.GRPC.Metadata["x-token"] != "secret" || request.GRPC.ProtoFiles[0] != "service.proto" ||
		request.GRPC.ImportPaths[0] != "proto" || request.GRPC.TLS.CAFile != "ca.pem" {
		t.Fatalf("clone shares nested gRPC state: original = %#v", request.GRPC)
	}
}
