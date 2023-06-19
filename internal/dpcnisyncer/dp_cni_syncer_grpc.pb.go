/*
 * Copyright(c) Red Hat Inc.
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

// Code generated by protoc-gen-go-grpc. DO NOT EDIT.
// versions:
// - protoc-gen-go-grpc v1.3.0
// - protoc             v3.12.4
// source: dp_cni_syncer.proto

package dpcnisyncer

import (
	context "context"
	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
// Requires gRPC-Go v1.32.0 or later.
const _ = grpc.SupportPackageIsVersion7

const (
	NetDev_DelNetDev_FullMethodName = "/dpcnisyncer.NetDev/DelNetDev"
)

// NetDevClient is the client API for NetDev service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://pkg.go.dev/google.golang.org/grpc/?tab=doc#ClientConn.NewStream.
type NetDevClient interface {
	DelNetDev(ctx context.Context, in *DeleteNetDevReq, opts ...grpc.CallOption) (*DeleteNetDevResp, error)
}

type netDevClient struct {
	cc grpc.ClientConnInterface
}

func NewNetDevClient(cc grpc.ClientConnInterface) NetDevClient {
	return &netDevClient{cc}
}

func (c *netDevClient) DelNetDev(ctx context.Context, in *DeleteNetDevReq, opts ...grpc.CallOption) (*DeleteNetDevResp, error) {
	out := new(DeleteNetDevResp)
	err := c.cc.Invoke(ctx, NetDev_DelNetDev_FullMethodName, in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// NetDevServer is the server API for NetDev service.
// All implementations must embed UnimplementedNetDevServer
// for forward compatibility
type NetDevServer interface {
	DelNetDev(context.Context, *DeleteNetDevReq) (*DeleteNetDevResp, error)
	mustEmbedUnimplementedNetDevServer()
}

// UnimplementedNetDevServer must be embedded to have forward compatible implementations.
type UnimplementedNetDevServer struct {
}

func (UnimplementedNetDevServer) DelNetDev(context.Context, *DeleteNetDevReq) (*DeleteNetDevResp, error) {
	return nil, status.Errorf(codes.Unimplemented, "method DelNetDev not implemented")
}
func (UnimplementedNetDevServer) mustEmbedUnimplementedNetDevServer() {}

// UnsafeNetDevServer may be embedded to opt out of forward compatibility for this service.
// Use of this interface is not recommended, as added methods to NetDevServer will
// result in compilation errors.
type UnsafeNetDevServer interface {
	mustEmbedUnimplementedNetDevServer()
}

func RegisterNetDevServer(s grpc.ServiceRegistrar, srv NetDevServer) {
	s.RegisterService(&NetDev_ServiceDesc, srv)
}

func _NetDev_DelNetDev_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(DeleteNetDevReq)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(NetDevServer).DelNetDev(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: NetDev_DelNetDev_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(NetDevServer).DelNetDev(ctx, req.(*DeleteNetDevReq))
	}
	return interceptor(ctx, in, info, handler)
}

// NetDev_ServiceDesc is the grpc.ServiceDesc for NetDev service.
// It's only intended for direct use with grpc.RegisterService,
// and not to be introspected or modified (even as a copy)
var NetDev_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "dpcnisyncer.NetDev",
	HandlerType: (*NetDevServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "DelNetDev",
			Handler:    _NetDev_DelNetDev_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "dp_cni_syncer.proto",
}