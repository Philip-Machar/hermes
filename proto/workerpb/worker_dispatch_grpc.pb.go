// Code generated manually — do not edit.
// Provides the WorkerDispatchService gRPC client/server stubs.
// This is a separate service from WorkerService.
// Workers host this service so the API can push jobs directly to them.

package workerpb

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Method name used on the wire.
const WorkerDispatchService_DispatchJob_FullMethodName = "/worker.WorkerDispatchService/DispatchJob"

// ----- Client ----------------------------------------------------------------

// WorkerDispatchServiceClient is used by the API server to call DispatchJob on a worker.
type WorkerDispatchServiceClient interface {
	DispatchJob(ctx context.Context, in *DispatchRequest, opts ...grpc.CallOption) (*DispatchResponse, error)
}

type workerDispatchServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewWorkerDispatchServiceClient(cc grpc.ClientConnInterface) WorkerDispatchServiceClient {
	return &workerDispatchServiceClient{cc}
}

func (c *workerDispatchServiceClient) DispatchJob(ctx context.Context, in *DispatchRequest, opts ...grpc.CallOption) (*DispatchResponse, error) {
	cOpts := append([]grpc.CallOption{grpc.StaticMethod()}, opts...)
	out := new(DispatchResponse)
	err := c.cc.Invoke(ctx, WorkerDispatchService_DispatchJob_FullMethodName, in, out, cOpts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ----- Server ----------------------------------------------------------------

// WorkerDispatchServiceServer is implemented by the worker to receive jobs from the API.
type WorkerDispatchServiceServer interface {
	DispatchJob(context.Context, *DispatchRequest) (*DispatchResponse, error)
	mustEmbedUnimplementedWorkerDispatchServiceServer()
}

// UnimplementedWorkerDispatchServiceServer must be embedded in any implementation.
type UnimplementedWorkerDispatchServiceServer struct{}

func (UnimplementedWorkerDispatchServiceServer) DispatchJob(context.Context, *DispatchRequest) (*DispatchResponse, error) {
	return nil, status.Error(codes.Unimplemented, "method DispatchJob not implemented")
}

func (UnimplementedWorkerDispatchServiceServer) mustEmbedUnimplementedWorkerDispatchServiceServer() {
}

func (UnimplementedWorkerDispatchServiceServer) testEmbeddedByValue() {}

// RegisterWorkerDispatchServiceServer registers the service on a gRPC server.
func RegisterWorkerDispatchServiceServer(s grpc.ServiceRegistrar, srv WorkerDispatchServiceServer) {
	if t, ok := srv.(interface{ testEmbeddedByValue() }); ok {
		t.testEmbeddedByValue()
	}
	s.RegisterService(&WorkerDispatchService_ServiceDesc, srv)
}

func _WorkerDispatchService_DispatchJob_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(DispatchRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(WorkerDispatchServiceServer).DispatchJob(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: WorkerDispatchService_DispatchJob_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(WorkerDispatchServiceServer).DispatchJob(ctx, req.(*DispatchRequest))
	}
	return interceptor(ctx, in, info, handler)
}

// WorkerDispatchService_ServiceDesc is the service descriptor for WorkerDispatchService.
var WorkerDispatchService_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "worker.WorkerDispatchService",
	HandlerType: (*WorkerDispatchServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "DispatchJob",
			Handler:    _WorkerDispatchService_DispatchJob_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "worker_dispatch.proto",
}
