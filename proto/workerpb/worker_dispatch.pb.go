// Code generated manually — do not edit.
// Equivalent to running protoc on the following proto:
//
//	syntax = "proto3";
//	package worker;
//	option go_package = "./proto/workerpb;workerpb";
//	message DispatchRequest  { string job_id=1; string job_type=2; string payload=3; }
//	message DispatchResponse { string status=1; }

package workerpb

import (
	"reflect"
	"unsafe"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/runtime/protoimpl"
)

// DispatchRequest is sent by the API to a worker to execute a job directly.
type DispatchRequest struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	JobId         string                 `protobuf:"bytes,1,opt,name=job_id,json=jobId,proto3" json:"job_id,omitempty"`
	JobType       string                 `protobuf:"bytes,2,opt,name=job_type,json=jobType,proto3" json:"job_type,omitempty"`
	Payload       string                 `protobuf:"bytes,3,opt,name=payload,proto3" json:"payload,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *DispatchRequest) Reset()         { *x = DispatchRequest{} }
func (x *DispatchRequest) String() string { return protoimpl.X.MessageStringOf(x) }
func (*DispatchRequest) ProtoMessage()    {}

func (x *DispatchRequest) ProtoReflect() protoreflect.Message {
	mi := &file_worker_dispatch_proto_msgTypes[0]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

func (x *DispatchRequest) GetJobId() string {
	if x != nil {
		return x.JobId
	}
	return ""
}

func (x *DispatchRequest) GetJobType() string {
	if x != nil {
		return x.JobType
	}
	return ""
}

func (x *DispatchRequest) GetPayload() string {
	if x != nil {
		return x.Payload
	}
	return ""
}

// DispatchResponse is returned by the worker after receiving a dispatch call.
type DispatchResponse struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	Status        string                 `protobuf:"bytes,1,opt,name=status,proto3" json:"status,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *DispatchResponse) Reset()         { *x = DispatchResponse{} }
func (x *DispatchResponse) String() string { return protoimpl.X.MessageStringOf(x) }
func (*DispatchResponse) ProtoMessage()    {}

func (x *DispatchResponse) ProtoReflect() protoreflect.Message {
	mi := &file_worker_dispatch_proto_msgTypes[1]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

func (x *DispatchResponse) GetStatus() string {
	if x != nil {
		return x.Status
	}
	return ""
}

// ----- file descriptor (verified by decoding) --------------------------------
//
// Encodes FileDescriptorProto for worker_dispatch.proto.
// Verified fields: name, package, two message types, go_package option, syntax.

var File_worker_dispatch_proto protoreflect.FileDescriptor

const file_worker_dispatch_proto_rawDesc = "\x0a\x15\x77\x6f\x72\x6b\x65\x72\x5f\x64\x69\x73\x70\x61\x74\x63" +
	"\x68\x2e\x70\x72\x6f\x74\x6f\x12\x06\x77\x6f\x72\x6b\x65\x72\x22" +
	"\x44\x0a\x0f\x44\x69\x73\x70\x61\x74\x63\x68\x52\x65\x71\x75\x65" +
	"\x73\x74\x12\x0e\x0a\x06\x6a\x6f\x62\x5f\x69\x64\x18\x01\x20\x01" +
	"\x28\x09\x12\x10\x0a\x08\x6a\x6f\x62\x5f\x74\x79\x70\x65\x18\x02" +
	"\x20\x01\x28\x09\x12\x0f\x0a\x07\x70\x61\x79\x6c\x6f\x61\x64\x18" +
	"\x03\x20\x01\x28\x09\x22\x22\x0a\x10\x44\x69\x73\x70\x61\x74\x63" +
	"\x68\x52\x65\x73\x70\x6f\x6e\x73\x65\x12\x0e\x0a\x06\x73\x74\x61" +
	"\x74\x75\x73\x18\x01\x20\x01\x28\x09\x42\x1b\x5a\x19\x2e\x2f\x70" +
	"\x72\x6f\x74\x6f\x2f\x77\x6f\x72\x6b\x65\x72\x70\x62\x3b\x77\x6f" +
	"\x72\x6b\x65\x72\x70\x62\x62\x06\x70\x72\x6f\x74\x6f\x33"

var file_worker_dispatch_proto_msgTypes = make([]protoimpl.MessageInfo, 2)

var file_worker_dispatch_proto_goTypes = []any{
	(*DispatchRequest)(nil),  // 0
	(*DispatchResponse)(nil), // 1
}

var file_worker_dispatch_proto_depIdxs = []int32{
	0, // placeholder — no cross-type references
}

func init() { file_worker_dispatch_proto_init() }

func file_worker_dispatch_proto_init() {
	if File_worker_dispatch_proto != nil {
		return
	}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: unsafe.Slice(unsafe.StringData(file_worker_dispatch_proto_rawDesc), len(file_worker_dispatch_proto_rawDesc)),
			NumEnums:      0,
			NumMessages:   2,
			NumExtensions: 0,
			NumServices:   0,
		},
		GoTypes:           file_worker_dispatch_proto_goTypes,
		DependencyIndexes: file_worker_dispatch_proto_depIdxs,
		MessageInfos:      file_worker_dispatch_proto_msgTypes,
	}.Build()
	File_worker_dispatch_proto = out.File
	file_worker_dispatch_proto_goTypes = nil
	file_worker_dispatch_proto_depIdxs = nil
}
