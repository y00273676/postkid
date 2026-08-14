package grpcengine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

func TestLoadProtoFilesAndDiscoverLocally(t *testing.T) {
	dir := t.TempDir()
	commonPath := filepath.Join(dir, "common.proto")
	servicePath := filepath.Join(dir, "service.proto")
	if err := os.WriteFile(commonPath, []byte(`syntax = "proto3";
package demo;

message Request { string value = 1; }
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(servicePath, []byte(`syntax = "proto3";
package demo;
import "common.proto";

message Response { string value = 1; }
service Echo { rpc Ping(Request) returns (Response); }
`), 0o600); err != nil {
		t.Fatal(err)
	}

	source, err := LoadProtoFiles([]string{servicePath}, []string{dir})
	if err != nil {
		t.Fatalf("LoadProtoFiles: %v", err)
	}
	services, err := ServicesFromSource(source)
	if err != nil {
		t.Fatalf("ServicesFromSource: %v", err)
	}
	if len(services) != 1 || services[0].Name != "demo.Echo" {
		t.Fatalf("services = %#v", services)
	}
	if len(services[0].Methods) != 1 || services[0].Methods[0].Name != "Ping" {
		t.Fatalf("methods = %#v", services[0].Methods)
	}
}

func TestLoadProtoFilesUsesMultipleImportPaths(t *testing.T) {
	dir := t.TempDir()
	imports := filepath.Join(dir, "imports")
	serviceDir := filepath.Join(dir, "service")
	if err := os.MkdirAll(imports, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(serviceDir, 0o700); err != nil {
		t.Fatal(err)
	}
	shared := filepath.Join(imports, "shared.proto")
	service := filepath.Join(serviceDir, "service.proto")
	if err := os.WriteFile(shared, []byte(`syntax = "proto3";
package shared;
message Request { string value = 1; }
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(service, []byte(`syntax = "proto3";
package demo;
import "shared.proto";
message Response { string value = 1; }
service Echo { rpc Ping(shared.Request) returns (Response); }
`), 0o600); err != nil {
		t.Fatal(err)
	}
	source, err := LoadProtoFiles([]string{service}, []string{imports, serviceDir})
	if err != nil {
		t.Fatalf("LoadProtoFiles: %v", err)
	}
	services, err := ServicesFromSource(source)
	if err != nil || len(services) != 1 || services[0].Methods[0].InputType != "shared.Request" {
		t.Fatalf("services = %#v, err = %v", services, err)
	}
}

func TestLoadDescriptorSetResolvesDependenciesOutOfOrder(t *testing.T) {
	request := &descriptorpb.DescriptorProto{Name: proto.String("Request")}
	response := &descriptorpb.DescriptorProto{Name: proto.String("Response")}
	common := &descriptorpb.FileDescriptorProto{
		Name:        proto.String("common.proto"),
		Package:     proto.String("demo"),
		Syntax:      proto.String("proto3"),
		MessageType: []*descriptorpb.DescriptorProto{request},
	}
	service := &descriptorpb.FileDescriptorProto{
		Name:       proto.String("service.proto"),
		Package:    proto.String("demo"),
		Syntax:     proto.String("proto3"),
		Dependency: []string{"common.proto"},
		MessageType: []*descriptorpb.DescriptorProto{
			response,
		},
		Service: []*descriptorpb.ServiceDescriptorProto{{
			Name: proto.String("Echo"),
			Method: []*descriptorpb.MethodDescriptorProto{{
				Name:       proto.String("Ping"),
				InputType:  proto.String(".demo.Request"),
				OutputType: proto.String(".demo.Response"),
			}},
		}},
	}
	setPath := filepath.Join(t.TempDir(), "demo.protoset")
	data, err := proto.Marshal(&descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{service, common}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(setPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	source, err := LoadDescriptorSet(setPath)
	if err != nil {
		t.Fatalf("LoadDescriptorSet: %v", err)
	}
	services, err := ServicesFromSource(source)
	if err != nil || len(services) != 1 || services[0].Name != "demo.Echo" {
		t.Fatalf("services = %#v, err = %v", services, err)
	}
}

func TestLoadDescriptorSourceRejectsAmbiguousConfig(t *testing.T) {
	_, err := LoadDescriptorSource(DescriptorConfig{DescriptorSet: "demo.protoset", ProtoFiles: []string{"demo.proto"}})
	if err == nil || !strings.Contains(err.Error(), "cannot be combined") {
		t.Fatalf("error = %v, want mixed-source validation error", err)
	}
	_, err = LoadDescriptorSource(DescriptorConfig{ImportPaths: []string{"proto"}})
	if err == nil || !strings.Contains(err.Error(), "require proto_files") {
		t.Fatalf("error = %v, want orphan import-path validation error", err)
	}
}

func TestLoadProtoFilesRejectsImportEscape(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "service.proto")
	if err := os.WriteFile(file, []byte(`syntax = "proto3";
package demo;
import "../outside.proto";
service Echo { rpc Ping(Request) returns (Request); }
message Request {}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadProtoFiles([]string{file}, []string{dir})
	if err == nil || !strings.Contains(err.Error(), "escapes configured") {
		t.Fatalf("error = %v, want import escape error", err)
	}
}

func TestLoadProtoFilesRejectsDuplicateImportNames(t *testing.T) {
	dir := t.TempDir()
	firstDir := filepath.Join(dir, "first")
	secondDir := filepath.Join(dir, "second")
	if err := os.MkdirAll(firstDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(secondDir, 0o700); err != nil {
		t.Fatal(err)
	}
	first := filepath.Join(firstDir, "service.proto")
	second := filepath.Join(secondDir, "service.proto")
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, []byte(`syntax = "proto3"; package demo; message Request {}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	_, err := LoadProtoFiles([]string{first, second}, nil)
	if err == nil || !strings.Contains(err.Error(), "same import name") {
		t.Fatalf("error = %v, want duplicate import-name error", err)
	}
}
