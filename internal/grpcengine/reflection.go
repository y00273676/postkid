package grpcengine

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	reflectpb "google.golang.org/grpc/reflection/grpc_reflection_v1alpha"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
)

func listServices(ctx context.Context, conn *grpc.ClientConn, md map[string]string) ([]string, error) {
	stream, err := reflectpb.NewServerReflectionClient(conn).ServerReflectionInfo(withOutgoingMetadata(ctx, md))
	if err != nil {
		return nil, fmt.Errorf("open reflection stream: %w", err)
	}
	request := &reflectpb.ServerReflectionRequest{
		MessageRequest: &reflectpb.ServerReflectionRequest_ListServices{ListServices: ""},
	}
	if err := stream.Send(request); err != nil {
		return nil, fmt.Errorf("send reflection list request: %w", err)
	}
	response, err := stream.Recv()
	if err != nil {
		return nil, fmt.Errorf("receive reflection list response: %w", err)
	}
	if err := reflectionError(response); err != nil {
		return nil, err
	}
	list := response.GetListServicesResponse()
	if list == nil {
		return nil, fmt.Errorf("reflection returned no service list")
	}
	services := make([]string, 0, len(list.Service))
	for _, service := range list.Service {
		if service.GetName() != "" {
			services = append(services, service.GetName())
		}
	}
	return services, nil
}

func filesForSymbol(ctx context.Context, conn *grpc.ClientConn, md map[string]string, symbol string) ([]*descriptorpb.FileDescriptorProto, error) {
	stream, err := reflectpb.NewServerReflectionClient(conn).ServerReflectionInfo(withOutgoingMetadata(ctx, md))
	if err != nil {
		return nil, fmt.Errorf("open reflection stream: %w", err)
	}
	request := &reflectpb.ServerReflectionRequest{
		MessageRequest: &reflectpb.ServerReflectionRequest_FileContainingSymbol{FileContainingSymbol: symbol},
	}
	if err := stream.Send(request); err != nil {
		return nil, fmt.Errorf("send reflection descriptor request: %w", err)
	}
	response, err := stream.Recv()
	if err != nil {
		return nil, fmt.Errorf("receive reflection descriptor response: %w", err)
	}
	if err := reflectionError(response); err != nil {
		return nil, err
	}
	descriptors := response.GetFileDescriptorResponse()
	if descriptors == nil {
		return nil, fmt.Errorf("reflection returned no descriptors for %q", symbol)
	}
	files := make([]*descriptorpb.FileDescriptorProto, 0, len(descriptors.FileDescriptorProto))
	for i, data := range descriptors.FileDescriptorProto {
		file := &descriptorpb.FileDescriptorProto{}
		if err := proto.Unmarshal(data, file); err != nil {
			return nil, fmt.Errorf("decode reflected descriptor %d for %q: %w", i, symbol, err)
		}
		files = append(files, file)
	}
	return files, nil
}

func reflectionError(response *reflectpb.ServerReflectionResponse) error {
	if response == nil {
		return fmt.Errorf("reflection returned an empty response")
	}
	if errResponse := response.GetErrorResponse(); errResponse != nil {
		return fmt.Errorf("reflection error %d: %s", errResponse.GetErrorCode(), errResponse.GetErrorMessage())
	}
	return nil
}

// buildRegistry creates dynamic descriptors while resolving well-known types
// from the global registry. Reflection implementations commonly omit those
// well-known files even though they are imported by a service's proto.
func buildRegistry(files []*descriptorpb.FileDescriptorProto) (*protoregistry.Files, error) {
	byPath := make(map[string]*descriptorpb.FileDescriptorProto, len(files))
	for _, file := range files {
		if file.GetName() == "" {
			return nil, fmt.Errorf("reflected descriptor has no file name")
		}
		byPath[file.GetName()] = file
	}
	registry := &protoregistry.Files{}
	visiting := make(map[string]bool, len(byPath))
	added := make(map[string]bool, len(byPath))
	var add func(string) error
	add = func(path string) error {
		if added[path] {
			return nil
		}
		if visiting[path] {
			return fmt.Errorf("descriptor import cycle at %q", path)
		}
		file, ok := byPath[path]
		if !ok {
			if _, err := protoregistry.GlobalFiles.FindFileByPath(path); err == nil {
				return nil
			}
			return fmt.Errorf("reflected descriptor dependency %q not found", path)
		}
		visiting[path] = true
		for _, dep := range file.GetDependency() {
			if err := add(dep); err != nil {
				return err
			}
		}
		resolver := registryResolver{local: registry}
		desc, err := protodesc.NewFile(file, resolver)
		if err != nil {
			return fmt.Errorf("construct descriptor %q: %w", path, err)
		}
		if err := registry.RegisterFile(desc); err != nil {
			return fmt.Errorf("register descriptor %q: %w", path, err)
		}
		delete(visiting, path)
		added[path] = true
		return nil
	}
	for path := range byPath {
		if err := add(path); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

type registryResolver struct{ local *protoregistry.Files }

func (r registryResolver) FindFileByPath(path string) (protoreflect.FileDescriptor, error) {
	if file, err := r.local.FindFileByPath(path); err == nil {
		return file, nil
	}
	return protoregistry.GlobalFiles.FindFileByPath(path)
}

func (r registryResolver) FindDescriptorByName(name protoreflect.FullName) (protoreflect.Descriptor, error) {
	if desc, err := r.local.FindDescriptorByName(name); err == nil {
		return desc, nil
	}
	return protoregistry.GlobalFiles.FindDescriptorByName(name)
}

func cloneMetadata(md metadata.MD) map[string][]string {
	result := make(map[string][]string, len(md))
	for key, values := range md {
		result[key] = append([]string(nil), values...)
	}
	return result
}
