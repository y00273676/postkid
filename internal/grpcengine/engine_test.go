package grpcengine

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/types/descriptorpb"

	"go.planetmeican.com/yangguang/postkid/internal/model"
	grpc_testing "google.golang.org/grpc/reflection/grpc_testing"
)

type reflectionSearchServer struct {
	grpc_testing.UnimplementedSearchServiceServer
}

func (reflectionSearchServer) Search(ctx context.Context, req *grpc_testing.SearchRequest) (*grpc_testing.SearchResponse, error) {
	if got := metadata.ValueFromIncomingContext(ctx, "x-request-id"); len(got) != 1 || got[0] != "test-123" {
		return nil, status.Errorf(3, "metadata was not propagated")
	}
	_ = grpc.SetHeader(ctx, metadata.Pairs("x-server-header", "yes"))
	grpc.SetTrailer(ctx, metadata.Pairs("x-server-trailer", "done"))
	return &grpc_testing.SearchResponse{Results: []*grpc_testing.SearchResponse_Result{{
		Url: "https://example.test/" + req.GetQuery(), Title: "result",
	}}}, nil
}

func (reflectionSearchServer) StreamingSearch(grpc.BidiStreamingServer[grpc_testing.SearchRequest, grpc_testing.SearchResponse]) error {
	return status.Error(12, "not used")
}

func startReflectionServer(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	grpc_testing.RegisterSearchServiceServer(server, reflectionSearchServer{})
	reflection.Register(server)
	go func() {
		_ = server.Serve(listener)
	}()
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
	})
	return listener.Addr().String()
}

func startNoReflectionServer(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	grpc_testing.RegisterSearchServiceServer(server, reflectionSearchServer{})
	go func() {
		_ = server.Serve(listener)
	}()
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
	})
	return listener.Addr().String()
}

func TestDiscoverAndInvokeUnaryWithReflection(t *testing.T) {
	target := startReflectionServer(t)
	engine := New()
	services, err := engine.Discover(context.Background(), target, model.GRPCTLSConfig{}, nil)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	var foundService model.GRPCService
	for _, service := range services {
		if service.Name == "grpc.testing.SearchService" {
			foundService = service
			break
		}
	}
	if len(foundService.Methods) != 2 {
		t.Fatalf("SearchService methods = %d, want unary + streaming", len(foundService.Methods))
	}
	var foundStreaming bool
	for _, method := range foundService.Methods {
		if method.Name == "StreamingSearch" {
			foundStreaming = method.ClientStreaming && method.ServerStreaming
		}
	}
	if !foundStreaming {
		t.Fatal("reflection did not report StreamingSearch as bidirectional")
	}

	response := engine.Invoke(context.Background(), model.ResolvedGRPCRequest{
		Target:   target,
		Method:   "grpc.testing.SearchService/Search",
		Body:     `{"query":"books"}`,
		Metadata: map[string]string{"x-request-id": "test-123"},
	})
	if response.Err != nil {
		t.Fatalf("Invoke: %v", response.Err)
	}
	if response.Status != "OK" || !strings.Contains(response.Body, "example.test/books") {
		t.Fatalf("response = %#v", response)
	}
	if response.Headers["x-server-header"][0] != "yes" || response.Trailers["x-server-trailer"][0] != "done" {
		t.Fatalf("metadata = headers %#v trailers %#v", response.Headers, response.Trailers)
	}
}

func TestInvokeRejectsStreaming(t *testing.T) {
	target := startReflectionServer(t)
	response := New().Invoke(context.Background(), model.ResolvedGRPCRequest{
		Target: target,
		Method: "grpc.testing.SearchService/StreamingSearch",
		Body:   `{}`,
	})
	if response.Err == nil || !strings.Contains(response.Err.Error(), "streaming method") {
		t.Fatalf("error = %v, want unsupported streaming error", response.Err)
	}
}

func TestInvokeWithLocalProtoWithoutReflection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "search.proto")
	const source = `syntax = "proto3";
package grpc.testing;

message SearchRequest { string query = 1; }
message SearchResponse {
  message Result { string url = 1; string title = 2; repeated string snippets = 3; }
  repeated Result results = 1;
}
service SearchService {
  rpc Search(SearchRequest) returns (SearchResponse);
}
`
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	target := startNoReflectionServer(t)
	response := New().Invoke(context.Background(), model.ResolvedGRPCRequest{
		Target:      target,
		Method:      "grpc.testing.SearchService/Search",
		Body:        `{"query":"local"}`,
		Metadata:    map[string]string{"x-request-id": "test-123"},
		ProtoFiles:  []string{path},
		ImportPaths: []string{dir},
	})
	if response.Err != nil {
		t.Fatalf("Invoke without reflection: %v", response.Err)
	}
	if !strings.Contains(response.Body, "example.test/local") {
		t.Fatalf("response = %#v", response)
	}
}

func TestInvokeWithDescriptorSetWithoutReflection(t *testing.T) {
	descriptorSet := &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{
		protodesc.ToFileDescriptorProto(grpc_testing.File_reflection_grpc_testing_test_proto),
	}}
	data, err := proto.Marshal(descriptorSet)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "search.protoset")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	response := New().Invoke(context.Background(), model.ResolvedGRPCRequest{
		Target:        startNoReflectionServer(t),
		Method:        "grpc.testing.SearchService/Search",
		Body:          `{"query":"protoset"}`,
		Metadata:      map[string]string{"x-request-id": "test-123"},
		DescriptorSet: path,
	})
	if response.Err != nil {
		t.Fatalf("Invoke with descriptor set and no reflection: %v", response.Err)
	}
	if !strings.Contains(response.Body, "example.test/protoset") {
		t.Fatalf("response = %#v", response)
	}
}

func TestDiscoverRequestWithLocalDescriptorsDoesNotDialTarget(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "local.proto")
	if err := os.WriteFile(path, []byte(`syntax = "proto3";
package local;
message Request {}
service Offline { rpc Check(Request) returns (Request); }
`), 0o600); err != nil {
		t.Fatal(err)
	}
	services, err := New().DiscoverRequest(context.Background(), model.ResolvedGRPCRequest{
		Target:     "this target is intentionally invalid",
		ProtoFiles: []string{path},
	})
	if err != nil {
		t.Fatalf("local discovery: %v", err)
	}
	if len(services) != 1 || services[0].Name != "local.Offline" {
		t.Fatalf("services = %#v", services)
	}
}

func TestNormalizeTargetAndMethod(t *testing.T) {
	target, tlsConfig, err := NormalizeTarget("grpcs://example.test:443", model.GRPCTLSConfig{})
	if err != nil || target != "example.test:443" || !tlsConfig.Enabled {
		t.Fatalf("NormalizeTarget = %q, %#v, %v", target, tlsConfig, err)
	}
	service, method, err := NormalizeMethod("", "/demo.Echo/Ping")
	if err != nil || service != "demo.Echo" || method != "Ping" {
		t.Fatalf("NormalizeMethod = %q/%q, %v", service, method, err)
	}
}
