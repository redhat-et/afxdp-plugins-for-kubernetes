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

package bpfman

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"

	v1 "github.com/redhat-et/afxdp-plugins-for-kubernetes/internal/bpfman/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

/*
fakeServer implements the bpfman gRPC service in memory for client tests.
*/
type fakeServer struct {
	v1.UnimplementedBpfmanServer

	mu           sync.Mutex
	nextID       uint32
	programs     map[uint32]*v1.ProgramInfo
	attached     map[uint32]*v1.XDPAttachInfo
	loadErr      error
	attachErr    error
	unloadErr    error
	unloadedIDs  []uint32
	attachedMeta map[uint32]map[string]string
}

func newFakeServer() *fakeServer {
	return &fakeServer{
		nextID:       200,
		programs:     make(map[uint32]*v1.ProgramInfo),
		attached:     make(map[uint32]*v1.XDPAttachInfo),
		attachedMeta: make(map[uint32]map[string]string),
	}
}

func (s *fakeServer) Load(_ context.Context, req *v1.LoadRequest) (*v1.LoadResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.loadErr != nil {
		return nil, s.loadErr
	}

	s.nextID++
	id := s.nextID

	s.programs[id] = &v1.ProgramInfo{
		Name:       req.GetInfo()[0].GetName(),
		Metadata:   req.GetMetadata(),
		MapPinPath: filepath.Join("/run/bpfman/fs/maps", itoa(id)),
	}

	return &v1.LoadResponse{
		Programs: []*v1.LoadResponseInfo{
			{
				Info:       s.programs[id],
				KernelInfo: &v1.KernelProgramInfo{Id: id},
			},
		},
	}, nil
}

func (s *fakeServer) Attach(_ context.Context, req *v1.AttachRequest) (*v1.AttachResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.attachErr != nil {
		return nil, s.attachErr
	}

	xdpInfo := req.GetAttach().GetXdpAttachInfo()
	s.attached[req.GetId()] = xdpInfo
	s.attachedMeta[req.GetId()] = xdpInfo.GetMetadata()

	return &v1.AttachResponse{LinkId: req.GetId()}, nil
}

func (s *fakeServer) Unload(_ context.Context, req *v1.UnloadRequest) (*v1.UnloadResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.unloadErr != nil {
		return nil, s.unloadErr
	}

	if _, ok := s.programs[req.GetId()]; !ok {
		return nil, status.Errorf(codes.NotFound, "program %d not found", req.GetId())
	}

	delete(s.programs, req.GetId())
	delete(s.attached, req.GetId())
	s.unloadedIDs = append(s.unloadedIDs, req.GetId())

	return &v1.UnloadResponse{}, nil
}

func (s *fakeServer) List(_ context.Context, req *v1.ListRequest) (*v1.ListResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	resp := &v1.ListResponse{}
	for id, info := range s.programs {
		if req.GetBpfmanProgramsOnly() && info.GetMetadata()[MetadataKeyOwner] != OwnerValue {
			continue
		}
		matched := true
		for k, v := range req.GetMatchMetadata() {
			if info.GetMetadata()[k] != v {
				matched = false
				break
			}
		}
		if !matched {
			continue
		}
		resp.Results = append(resp.GetResults(), &v1.ListResponse_ListResult{
			Info:       info,
			KernelInfo: &v1.KernelProgramInfo{Id: id},
		})
	}

	return resp, nil
}

func itoa(i uint32) string {
	return string(rune('0'+i/10)) + string(rune('0'+i%10))
}

/*
startFakeServer starts the fake bpfman gRPC service on a unix socket in a
temporary directory and returns the socket path.
*/
func startFakeServer(t *testing.T, srv *fakeServer) string {
	t.Helper()

	dir, err := os.MkdirTemp("", "bpfman-test-")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })

	socketPath := filepath.Join(dir, "bpfman.sock")
	listener, err := net.Listen("unix", socketPath)
	require.NoError(t, err)
	t.Cleanup(func() { listener.Close() })

	grpcServer := grpc.NewServer()
	v1.RegisterBpfmanServer(grpcServer, srv)
	go func() {
		_ = grpcServer.Serve(listener)
	}()
	t.Cleanup(grpcServer.Stop)

	return socketPath
}

func TestLoadAndAttachXDP(t *testing.T) {
	srv := newFakeServer()
	socketPath := startFakeServer(t, srv)

	handler, err := NewHandler(socketPath)
	require.NoError(t, err)
	t.Cleanup(func() { handler.Close() })

	ctx := context.Background()

	program, err := handler.LoadAndAttachXDP(ctx, XDPLoadOptions{
		BytecodeFile: "/afxdp/xdp_afxdp_redirect_bpfman.o",
		FunctionName: "xdp_afxdp_redirect",
		ProgramName:  "afxdp-myPool-ens1",
		Iface:        "ens1",
		Priority:     10,
		ExtraMetadata: map[string]string{
			MetadataKeyPool:   "myPool",
			MetadataKeyDevice: "ens1",
		},
	})
	require.NoError(t, err)
	assert.NotZero(t, program.ID)
	assert.Contains(t, program.MapPinPath, "/run/bpfman/fs/maps")

	srv.mu.Lock()
	defer srv.mu.Unlock()

	info := srv.programs[program.ID]
	require.NotNil(t, info)
	assert.Equal(t, "xdp_afxdp_redirect", info.GetName())
	assert.Equal(t, "afxdp-myPool-ens1", info.GetMetadata()[MetadataKeyProgramName])
	assert.Equal(t, OwnerValue, info.GetMetadata()[MetadataKeyOwner])
	assert.Equal(t, "myPool", info.GetMetadata()[MetadataKeyPool])

	attachInfo := srv.attached[program.ID]
	require.NotNil(t, attachInfo)
	assert.Equal(t, "ens1", attachInfo.GetIface())
	assert.Equal(t, int32(10), attachInfo.GetPriority())
	assert.Equal(t, []int32{2, 31}, attachInfo.GetProceedOn())
}

func TestLoadAndAttachXDPImage(t *testing.T) {
	srv := newFakeServer()
	socketPath := startFakeServer(t, srv)

	handler, err := NewHandler(socketPath)
	require.NoError(t, err)
	t.Cleanup(func() { handler.Close() })

	program, err := handler.LoadAndAttachXDP(context.Background(), XDPLoadOptions{
		BytecodeImage: "quay.io/afxdp/xdp-redirect:latest",
		FunctionName:  "xdp_afxdp_redirect",
		ProgramName:   "afxdp-myPool-ens1",
		Iface:         "ens1",
	})
	require.NoError(t, err)
	assert.NotZero(t, program.ID)
}

func TestLoadAndAttachXDPValidation(t *testing.T) {
	srv := newFakeServer()
	socketPath := startFakeServer(t, srv)

	handler, err := NewHandler(socketPath)
	require.NoError(t, err)
	t.Cleanup(func() { handler.Close() })

	ctx := context.Background()

	_, err = handler.LoadAndAttachXDP(ctx, XDPLoadOptions{
		BytecodeFile: "/afxdp/xdp_afxdp_redirect_bpfman.o",
		FunctionName: "xdp_afxdp_redirect",
		Iface:        "ens1",
	})
	assert.Error(t, err, "expected error when program name is missing")

	_, err = handler.LoadAndAttachXDP(ctx, XDPLoadOptions{
		BytecodeFile: "/afxdp/xdp_afxdp_redirect_bpfman.o",
		FunctionName: "xdp_afxdp_redirect",
	})
	assert.Error(t, err, "expected error when iface is missing")

	_, err = handler.LoadAndAttachXDP(ctx, XDPLoadOptions{
		BytecodeFile: "/afxdp/xdp_afxdp_redirect_bpfman.o",
		Iface:        "ens1",
	})
	assert.Error(t, err, "expected error when function name is missing")

	_, err = handler.LoadAndAttachXDP(ctx, XDPLoadOptions{
		FunctionName: "xdp_afxdp_redirect",
		Iface:        "ens1",
	})
	assert.Error(t, err, "expected error when no bytecode source is set")

	_, err = handler.LoadAndAttachXDP(ctx, XDPLoadOptions{
		BytecodeFile:  "/afxdp/xdp_afxdp_redirect_bpfman.o",
		BytecodeImage: "quay.io/afxdp/xdp-redirect:latest",
		FunctionName:  "xdp_afxdp_redirect",
		Iface:         "ens1",
	})
	assert.Error(t, err, "expected error when both bytecode sources are set")
}

func TestLoadAndAttachXDPAttachErrorUnloadsOrphan(t *testing.T) {
	srv := newFakeServer()
	socketPath := startFakeServer(t, srv)

	handler, err := NewHandler(socketPath)
	require.NoError(t, err)
	t.Cleanup(func() { handler.Close() })

	srv.mu.Lock()
	srv.attachErr = errors.New("attach failed")
	srv.mu.Unlock()

	_, err = handler.LoadAndAttachXDP(context.Background(), XDPLoadOptions{
		BytecodeFile: "/afxdp/xdp_afxdp_redirect_bpfman.o",
		FunctionName: "xdp_afxdp_redirect",
		ProgramName:  "afxdp-myPool-ens1",
		Iface:        "ens1",
	})
	require.Error(t, err)

	srv.mu.Lock()
	defer srv.mu.Unlock()
	assert.NotEmpty(t, srv.unloadedIDs, "orphaned program should be unloaded after a failed attach")
	assert.Empty(t, srv.programs, "no programs should remain loaded")
}

func TestUnload(t *testing.T) {
	srv := newFakeServer()
	socketPath := startFakeServer(t, srv)

	handler, err := NewHandler(socketPath)
	require.NoError(t, err)
	t.Cleanup(func() { handler.Close() })

	ctx := context.Background()

	program, err := handler.LoadAndAttachXDP(ctx, XDPLoadOptions{
		BytecodeFile: "/afxdp/xdp_afxdp_redirect_bpfman.o",
		FunctionName: "xdp_afxdp_redirect",
		ProgramName:  "afxdp-myPool-ens1",
		Iface:        "ens1",
	})
	require.NoError(t, err)

	require.NoError(t, handler.Unload(ctx, program.ID))

	srv.mu.Lock()
	assert.Empty(t, srv.programs, "program should be unloaded")
	srv.mu.Unlock()

	err = handler.Unload(ctx, program.ID)
	assert.Error(t, err, "unloading a non-existent program should fail")
}

func TestListXDPPrograms(t *testing.T) {
	srv := newFakeServer()
	socketPath := startFakeServer(t, srv)

	handler, err := NewHandler(socketPath)
	require.NoError(t, err)
	t.Cleanup(func() { handler.Close() })

	ctx := context.Background()

	for _, iface := range []string{"ens1", "ens2"} {
		_, err := handler.LoadAndAttachXDP(ctx, XDPLoadOptions{
			BytecodeFile: "/afxdp/xdp_afxdp_redirect_bpfman.o",
			FunctionName: "xdp_afxdp_redirect",
			ProgramName:  "afxdp-myPool-" + iface,
			Iface:        iface,
			ExtraMetadata: map[string]string{
				MetadataKeyPool:   "myPool",
				MetadataKeyDevice: iface,
			},
		})
		require.NoError(t, err)
	}

	programs, err := handler.ListXDPPrograms(ctx, map[string]string{
		MetadataKeyOwner: OwnerValue,
		MetadataKeyPool:  "myPool",
	})
	require.NoError(t, err)
	assert.Len(t, programs, 2)

	programs, err = handler.ListXDPPrograms(ctx, map[string]string{
		MetadataKeyOwner: OwnerValue,
		MetadataKeyPool:  "otherPool",
	})
	require.NoError(t, err)
	assert.Empty(t, programs)
}

func TestNewHandlerBadSocket(t *testing.T) {
	handler, err := NewHandler("/nonexistent/bpfman.sock")
	require.NoError(t, err, "grpc.NewClient is lazy and should not fail")

	_, err = handler.LoadAndAttachXDP(context.Background(), XDPLoadOptions{
		BytecodeFile: "/afxdp/xdp_afxdp_redirect_bpfman.o",
		FunctionName: "xdp_afxdp_redirect",
		ProgramName:  "afxdp-myPool-ens1",
		Iface:        "ens1",
	})
	assert.Error(t, err, "requests against a non-existent server should fail")
}
