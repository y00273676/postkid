package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"go.planetmeican.com/yangguang/postkid/internal/app"
	"go.planetmeican.com/yangguang/postkid/internal/config"
	"go.planetmeican.com/yangguang/postkid/internal/model"
)

func TestGRPCCommandOpensNewRequestForm(t *testing.T) {
	m := grpcTestModel(t)
	msg := m.executeCommand("grpc new")()
	if got, ok := msg.(GRPCOpenMsg); !ok || got.Edit || got.Discover {
		t.Fatalf("grpc new message = %#v, want a new non-discovery form", msg)
	}
	updated, _ := m.Update(msg)
	m = updated.(Model)
	if m.grpcModal == nil || m.grpcModal.edit {
		t.Fatalf("grpc modal = %#v, want new form", m.grpcModal)
	}
}

func TestGRPCNewRequestIsSavedToCollection(t *testing.T) {
	m := grpcTestModel(t)
	updated, _ := m.Update(GRPCOpenMsg{})
	m = updated.(Model)
	if m.grpcModal == nil {
		t.Fatal("gRPC modal did not open")
	}
	m.grpcModal.collection = 0
	m.grpcModal.name.SetValue("health-grpc")
	m.grpcModal.target.SetValue("127.0.0.1:50051")
	m.grpcModal.service.SetValue("health.Health")
	m.grpcModal.method.SetValue("Check")
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("saving a new gRPC request returned no command")
	}
	msg := cmd()
	if _, ok := msg.(ListUpdatedMsg); !ok {
		t.Fatalf("save command message = %T, want ListUpdatedMsg", msg)
	}
	updated, _ = m.Update(msg)
	m = updated.(Model)
	requests := m.app.Collections()[0].Requests
	if len(requests) != 1 {
		t.Fatalf("saved requests = %d, want 1", len(requests))
	}
	req := requests[0]
	if !req.IsGRPC() || req.Protocol != model.ProtocolGRPC {
		t.Fatalf("saved request = %#v, want grpc protocol", req)
	}
	if req.URL != "127.0.0.1:50051" || req.GRPC == nil || req.GRPC.Service != "health.Health" || req.GRPC.Method != "Check" {
		t.Fatalf("saved gRPC request = %#v", req)
	}
}

func TestGRPCDiscoverySelectsServiceAndMethod(t *testing.T) {
	m := grpcTestModel(t)
	updated, _ := m.Update(GRPCOpenMsg{})
	m = updated.(Model)
	services := []model.GRPCService{{
		Name: "health.Health",
		Methods: []model.GRPCMethod{
			{Name: "Check"},
			{Name: "Watch", ServerStreaming: true},
		},
	}}
	updated, _ = m.Update(GRPCDiscoveredMsg{Services: services})
	m = updated.(Model)
	if m.grpcModal.service.Value() != "health.Health" || m.grpcModal.method.Value() != "Check" {
		t.Fatalf("discovery selection = %q/%q", m.grpcModal.service.Value(), m.grpcModal.method.Value())
	}
	if !strings.Contains(stripANSI(m.View()), "streaming") {
		t.Fatalf("streaming method marker missing from modal view:\n%s", m.View())
	}
	// Down while the method field is focused selects the reflected streaming
	// method, which remains visible but is intentionally not hidden.
	m.grpcModal.focus = 4
	m.grpcModal.focusInputs()
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)
	if m.grpcModal.method.Value() != "Watch" {
		t.Fatalf("selected method = %q, want Watch", m.grpcModal.method.Value())
	}
}

func TestGRPCResponseRendersBodyAndStatus(t *testing.T) {
	m := grpcTestModel(t)
	m.width, m.height = 120, 40
	m.resize()
	updated, _ := m.Update(GRPCResponseMsg{Resp: model.GRPCResponse{
		Status: "OK",
		Body:   `{"ok":true}`,
		Size:   11,
	}})
	m = updated.(Model)
	plain := stripANSI(m.View())
	if !strings.Contains(plain, "gRPC OK") || !strings.Contains(plain, `"ok"`) {
		t.Fatalf("gRPC response missing status/body:\n%s", m.View())
	}
}

func TestGRPCStreamingErrorIsShown(t *testing.T) {
	m := grpcTestModel(t)
	m.width, m.height = 120, 40
	m.resize()
	streamErr := errors.New("gRPC streaming method /health.Health/Watch is unsupported; only unary RPCs are supported")
	updated, _ := m.Update(GRPCResponseMsg{Resp: model.GRPCResponse{
		Status: "Unimplemented",
		Err:    streamErr,
	}})
	m = updated.(Model)
	plain := stripANSI(m.View())
	if m.err == nil || !strings.Contains(plain, "streaming method") {
		t.Fatalf("streaming error missing from model/view: err=%v\n%s", m.err, m.View())
	}
}

func TestGRPCEditPreservesTLSFileConfiguration(t *testing.T) {
	req := &model.Request{
		Name: "secure", Protocol: model.ProtocolGRPC, URL: "api.example.test:443", Method: "Ping",
		GRPC: &model.GRPCRequest{Service: "demo.Echo", Method: "Ping", TLS: &model.GRPCTLSConfig{
			Enabled: true, CAFile: "/tmp/ca.pem", CertFile: "/tmp/client.pem", KeyFile: "/tmp/client.key",
		}},
	}
	modal := newGRPCModal(nil, nil, req, true)
	modal.serverName.SetValue("override.example.test")
	updated, err := modal.request(req)
	if err != nil {
		t.Fatal(err)
	}
	if updated.GRPC == nil || updated.GRPC.TLS == nil {
		t.Fatalf("updated TLS = %#v", updated.GRPC)
	}
	got := updated.GRPC.TLS
	if got.CAFile != "/tmp/ca.pem" || got.CertFile != "/tmp/client.pem" || got.KeyFile != "/tmp/client.key" {
		t.Fatalf("TLS file configuration was lost: %#v", got)
	}
}

func TestGRPCEditPreservesDescriptorAndMetadataConfiguration(t *testing.T) {
	req := &model.Request{
		Name: "local", Protocol: model.ProtocolGRPC, URL: "api.example.test:443", Method: "Get",
		Body:    `{"id":"42"}`,
		Headers: map[string]string{"x-legacy": "keep"},
		GRPC: &model.GRPCRequest{
			Service: "demo.Echo", Method: "Get",
			Metadata:    map[string]string{"authorization": "Bearer token"},
			ProtoFiles:  []string{"proto/echo.proto", "proto/common.proto"},
			ImportPaths: []string{"proto", "third_party"},
			TLS:         &model.GRPCTLSConfig{Enabled: true, CAFile: "/tmp/ca.pem", CertFile: "/tmp/client.pem", KeyFile: "/tmp/client.key"},
		},
	}
	modal := newGRPCModal(nil, nil, req, true)
	updated, err := modal.request(req)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Body != req.Body || updated.Headers["x-legacy"] != "keep" {
		t.Fatalf("edit lost request fields: %#v", updated)
	}
	if updated.GRPC == nil {
		t.Fatal("updated gRPC config is nil")
	}
	if got := updated.GRPC; !slicesEqual(got.ProtoFiles, req.GRPC.ProtoFiles) || !slicesEqual(got.ImportPaths, req.GRPC.ImportPaths) {
		t.Fatalf("descriptor fields changed during edit: %#v", got)
	}
	if got := updated.GRPC.Metadata["authorization"]; got != "Bearer token" {
		t.Fatalf("metadata = %q, want %q", got, "Bearer token")
	}
	if got := updated.GRPC.TLS; got == nil || got.CAFile != "/tmp/ca.pem" || got.CertFile != "/tmp/client.pem" || got.KeyFile != "/tmp/client.key" {
		t.Fatalf("TLS files changed during edit: %#v", got)
	}
}

func TestGRPCDescriptorSourceSwitchesAndSerializesExclusiveSource(t *testing.T) {
	modal := newGRPCModal(nil, nil, &model.Request{
		Name: "local", Protocol: model.ProtocolGRPC, URL: "127.0.0.1:50051", Method: "Get",
		GRPC: &model.GRPCRequest{Service: "demo.Echo", Method: "Get"},
	}, true)
	if modal.descriptorSource != grpcDescriptorReflection {
		t.Fatalf("default descriptor source = %v, want reflection", modal.descriptorSource)
	}
	modal.focus = 3 // target, service, method, descriptor source
	modal.focusInputs()
	modal.update(tea.KeyMsg{Type: tea.KeyRight})
	if modal.descriptorSource != grpcDescriptorProtoFiles {
		t.Fatalf("right from reflection source = %v, want proto files", modal.descriptorSource)
	}
	modal.protoFiles.SetValue("proto/echo.proto, proto/common.proto")
	modal.importPaths.SetValue("proto, third_party")
	updated, err := modal.request(&model.Request{Name: "local", Protocol: model.ProtocolGRPC, URL: "127.0.0.1:50051", Method: "Get", GRPC: &model.GRPCRequest{Service: "demo.Echo", Method: "Get"}})
	if err != nil {
		t.Fatal(err)
	}
	if !slicesEqual(updated.GRPC.ProtoFiles, []string{"proto/echo.proto", "proto/common.proto"}) || !slicesEqual(updated.GRPC.ImportPaths, []string{"proto", "third_party"}) || updated.GRPC.DescriptorSet != "" {
		t.Fatalf("proto source serialized incorrectly: %#v", updated.GRPC)
	}
	modal.update(tea.KeyMsg{Type: tea.KeyRight})
	if modal.descriptorSource != grpcDescriptorSet {
		t.Fatalf("right from proto source = %v, want protoset", modal.descriptorSource)
	}
	modal.descriptorSet.SetValue("descriptors/echo.protoset")
	updated, err = modal.request(&model.Request{Name: "local", Protocol: model.ProtocolGRPC, URL: "127.0.0.1:50051", Method: "Get", GRPC: &model.GRPCRequest{Service: "demo.Echo", Method: "Get"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.GRPC.ProtoFiles) != 0 || len(updated.GRPC.ImportPaths) != 0 || updated.GRPC.DescriptorSet != "descriptors/echo.protoset" {
		t.Fatalf("protoset source serialized incorrectly: %#v", updated.GRPC)
	}
}

func TestGRPCLocalDescriptorDiscoveryMessageDoesNotMentionReflection(t *testing.T) {
	m := grpcTestModel(t)
	updated, _ := m.Update(GRPCOpenMsg{})
	m = updated.(Model)
	m.grpcModal.descriptorSource = grpcDescriptorProtoFiles
	m.grpcModal.discovering = true
	plain := stripANSI(m.View())
	if !strings.Contains(plain, "discovering proto files") || strings.Contains(plain, "server reflection") {
		t.Fatalf("local descriptor discovery message = %q", plain)
	}
	updated, _ = m.Update(GRPCDiscoveredMsg{Services: []model.GRPCService{{Name: "demo.Echo"}}})
	m = updated.(Model)
	if !strings.Contains(m.statusMsg, "from proto files") || strings.Contains(m.statusMsg, "reflection") {
		t.Fatalf("local descriptor discovery status = %q", m.statusMsg)
	}
}

func TestGRPCDiscoveryAllowsEmptyServiceAndMethodWithLocalDescriptors(t *testing.T) {
	m := grpcTestModel(t)
	protoPath := filepath.Join(t.TempDir(), "echo.proto")
	proto := `syntax = "proto3";
package demo;

service Echo {
  rpc Get (GetRequest) returns (GetResponse);
}

message GetRequest {}
message GetResponse {}
`
	if err := os.WriteFile(protoPath, []byte(proto), 0o600); err != nil {
		t.Fatal(err)
	}
	req := &model.Request{
		Name: "discover", Protocol: model.ProtocolGRPC, URL: "127.0.0.1:1",
		GRPC: &model.GRPCRequest{Metadata: map[string]string{"x-request-id": "test"}},
	}
	m.curReq = req
	m.curColl = m.colls[0]
	m.grpcModal = newGRPCModal(m.colls, m.curColl, req, true)
	m.grpcModal.descriptorSource = grpcDescriptorProtoFiles
	m.grpcModal.protoFiles.SetValue(protoPath)
	m.grpcModal.importPaths.SetValue("")
	m.grpcModal.service.SetValue("")
	m.grpcModal.method.SetValue("")
	m.grpcDiscoverySeq = 1
	cmd := m.discoverGRPC(m.grpcDiscoverySeq)
	if cmd == nil {
		t.Fatal("discovery command is nil")
	}
	raw := cmd()
	msg, ok := raw.(GRPCDiscoveredMsg)
	if !ok {
		t.Fatalf("discovery result = %T, want GRPCDiscoveredMsg", raw)
	}
	if msg.Err != nil {
		t.Fatalf("local discovery failed with empty service/method: %v", msg.Err)
	}
	if len(msg.Services) != 1 || msg.Services[0].Name != "demo.Echo" {
		t.Fatalf("discovered services = %#v, want demo.Echo", msg.Services)
	}
}

func slicesEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func TestGRPCStaleDiscoveryResultIsIgnored(t *testing.T) {
	m := grpcTestModel(t)
	updated, _ := m.Update(GRPCOpenMsg{})
	m = updated.(Model)
	m.grpcDiscoverySeq = 2
	updated, _ = m.Update(GRPCDiscoveredMsg{Token: 1, Services: []model.GRPCService{{Name: "stale.Service"}}})
	m = updated.(Model)
	if len(m.grpcModal.services) != 0 {
		t.Fatalf("stale services applied: %#v", m.grpcModal.services)
	}
}

func grpcTestModel(t *testing.T) Model {
	t.Helper()
	cfg, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	appModel, err := app.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := appModel.CreateCollection("grpc"); err != nil {
		t.Fatal(err)
	}
	m := New(appModel)
	m.width, m.height = 120, 40
	m.resize()
	return m
}
