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

/*
Package bpfman provides the AF_XDP device plugin with a gRPC client for
bpfman (https://github.com/bpfman/bpfman).

In bpfman mode the device plugin no longer loads and attaches eBPF programs
itself. Instead, when a device pool is initialised, the XDP program that
redirects packets to AF_XDP sockets (XSKMAP redirect) is loaded and attached
on every netdev in the pool via bpfman, and unloaded when the pool is
terminated. This "pool lifecycle" model means the program is already in place
before kubelet mounts volumes for any pod scheduled onto the pool, which is
required for the bpfman CSI driver to expose the pinned XSKMAP into pods.

The generated gRPC stubs in the v1 subpackage are copied verbatim from
bpfman's clients/gobpfman/v1 (Apache-2.0) so that this repository builds
without depending on unreleased bpfman module versions.
*/
package bpfman

import (
	"context"
	"fmt"
	"time"

	v1 "github.com/redhat-et/afxdp-plugins-for-kubernetes/internal/bpfman/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	// MetadataKeyProgramName is the metadata key bpfman uses to identify a
	// program by name. It is matched by the bpfman CSI driver when a pod
	// requests maps from a program (csi.bpfman.io/program volume attribute).
	MetadataKeyProgramName = "bpfman.io/ProgramName"

	// Metadata keys used by the AF_XDP device plugin to mark the programs
	// it manages. They allow the plugin to reconcile (adopt or clean up)
	// its programs across restarts via the bpfman List RPC.
	MetadataKeyOwner  = "afxdp.io/owner"
	MetadataKeyPool   = "afxdp.io/pool"
	MetadataKeyDevice = "afxdp.io/device"

	// OwnerValue is the value set for MetadataKeyOwner on all programs
	// loaded by the AF_XDP device plugin.
	OwnerValue = "afxdp-device-plugin"
)

/*
XDPLoadOptions describes an XDP program load and attach request.
*/
type XDPLoadOptions struct {
	BytecodeFile  string            // path to a local .o file (mutually exclusive with BytecodeImage)
	BytecodeImage string            // URL of an OCI bytecode image (mutually exclusive with BytecodeFile)
	FunctionName  string            // program (function) name within the bytecode object
	ProgramName   string            // value for the bpfman.io/ProgramName metadata, used by the CSI driver
	Iface         string            // network interface to attach the program to
	Priority      int32             // XDP priority, lower runs first
	ProceedOn     []int32           // XDP actions the dispatcher chain continues on; empty uses bpfman defaults (pass, dispatcher_return)
	ExtraMetadata map[string]string // additional metadata merged into the load request
}

/*
ProgramSummary is a subset of program state returned by ListXDPPrograms.
*/
type ProgramSummary struct {
	ID         uint32
	Name       string
	Metadata   map[string]string
	MapPinPath string
}

/*
XDPProgram is the result of a successful load and attach.
*/
type XDPProgram struct {
	ID         uint32
	LinkID     uint32
	MapPinPath string
}

/*
Handler is the interface to the bpfman package.
The interface exists for testing purposes, allowing unit tests to run
without a bpfman daemon.
*/
type Handler interface {
	LoadAndAttachXDP(ctx context.Context, opts XDPLoadOptions) (*XDPProgram, error)
	Unload(ctx context.Context, id uint32) error
	ListXDPPrograms(ctx context.Context, matchMetadata map[string]string) ([]ProgramSummary, error)
	Close() error
}

/*
client implements the Handler interface against a bpfman gRPC server.
*/
type client struct {
	conn *grpc.ClientConn
	stub v1.BpfmanClient
}

/*
NewHandler returns an implementation of the Handler interface connected to
the bpfman gRPC server listening on the given unix socket path.
*/
func NewHandler(socketPath string) (Handler, error) {
	conn, err := grpc.NewClient("unix:"+socketPath, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("error connecting to bpfman on %s: %w", socketPath, err)
	}

	return &client{
		conn: conn,
		stub: v1.NewBpfmanClient(conn),
	}, nil
}

/*
LoadAndAttachXDP loads an XDP program via bpfman and attaches it to an
interface. The program is identified by the bpfman.io/ProgramName metadata
so that the bpfman CSI driver can expose its maps to pods.
*/
func (c *client) LoadAndAttachXDP(ctx context.Context, opts XDPLoadOptions) (*XDPProgram, error) {
	if opts.FunctionName == "" || opts.Iface == "" {
		return nil, fmt.Errorf("function name and interface are required")
	}
	if (opts.BytecodeFile == "") == (opts.BytecodeImage == "") {
		return nil, fmt.Errorf("exactly one of bytecode file or image must be set")
	}

	metadata := map[string]string{
		MetadataKeyProgramName: opts.ProgramName,
		MetadataKeyOwner:       OwnerValue,
	}
	for k, v := range opts.ExtraMetadata {
		metadata[k] = v
	}

	loadRequest := &v1.LoadRequest{
		Metadata: metadata,
		Info: []*v1.LoadInfo{
			{
				Name:        opts.FunctionName,
				ProgramType: v1.BpfmanProgramType_XDP,
			},
		},
	}
	if opts.BytecodeFile != "" {
		loadRequest.Bytecode = &v1.BytecodeLocation{
			Location: &v1.BytecodeLocation_File{File: opts.BytecodeFile},
		}
	} else {
		loadRequest.Bytecode = &v1.BytecodeLocation{
			Location: &v1.BytecodeLocation_Image{
				Image: &v1.BytecodeImage{Url: opts.BytecodeImage},
			},
		}
	}

	loadCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	loadRes, err := c.stub.Load(loadCtx, loadRequest)
	if err != nil {
		return nil, fmt.Errorf("error loading XDP program %s via bpfman: %w", opts.FunctionName, err)
	}

	if len(loadRes.Programs) != 1 {
		return nil, fmt.Errorf("expected 1 program in bpfman load response, got %d", len(loadRes.Programs))
	}

	prog := loadRes.Programs[0]
	if prog.GetKernelInfo() == nil {
		return nil, fmt.Errorf("bpfman load response missing kernel info")
	}
	programID := prog.GetKernelInfo().GetId()

	proceedOn := opts.ProceedOn
	if len(proceedOn) == 0 {
		// Match bpfman defaults: continue the dispatcher chain when the
		// program passes the packet (no socket attached to the queue),
		// or when the dispatcher itself returns.
		proceedOn = []int32{2 /* XDP pass */, 31 /* dispatcher return */}
	}

	attachCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	attachRes, err := c.stub.Attach(attachCtx, &v1.AttachRequest{
		Id: programID,
		Attach: &v1.AttachInfo{
			Info: &v1.AttachInfo_XdpAttachInfo{
				XdpAttachInfo: &v1.XDPAttachInfo{
					Priority:  opts.Priority,
					Iface:     opts.Iface,
					ProceedOn: proceedOn,
					Metadata:  metadata,
				},
			},
		},
	})
	if err != nil {
		// Unload the orphaned program; best effort.
		if _, unloadErr := c.stub.Unload(ctx, &v1.UnloadRequest{Id: programID}); unloadErr != nil {
			return nil, fmt.Errorf("error attaching XDP program %s to %s via bpfman: %w (unload of orphaned program %d failed: %v)",
				opts.FunctionName, opts.Iface, err, programID, unloadErr)
		}
		return nil, fmt.Errorf("error attaching XDP program %s to %s via bpfman: %w", opts.FunctionName, opts.Iface, err)
	}

	return &XDPProgram{
		ID:         programID,
		LinkID:     attachRes.GetLinkId(),
		MapPinPath: prog.GetInfo().GetMapPinPath(),
	}, nil
}

/*
Unload unloads a program previously loaded via bpfman. Unloading also
detaches any links held by the program.
*/
func (c *client) Unload(ctx context.Context, id uint32) error {
	unloadCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	if _, err := c.stub.Unload(unloadCtx, &v1.UnloadRequest{Id: id}); err != nil {
		return fmt.Errorf("error unloading program %d via bpfman: %w", id, err)
	}

	return nil
}

/*
ListXDPPrograms lists loaded XDP programs whose metadata matches all of the
given match metadata entries.
*/
func (c *client) ListXDPPrograms(ctx context.Context, matchMetadata map[string]string) ([]ProgramSummary, error) {
	listCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	listRes, err := c.stub.List(listCtx, &v1.ListRequest{
		ProgramType:        &[]uint32{uint32(v1.BpfmanProgramType_XDP)}[0],
		BpfmanProgramsOnly: &[]bool{true}[0],
		MatchMetadata:      matchMetadata,
	})
	if err != nil {
		return nil, fmt.Errorf("error listing bpfman XDP programs: %w", err)
	}

	programs := make([]ProgramSummary, 0, len(listRes.GetResults()))
	for _, result := range listRes.GetResults() {
		if result.GetKernelInfo() == nil {
			continue
		}
		programs = append(programs, ProgramSummary{
			ID:         result.GetKernelInfo().GetId(),
			Name:       result.GetInfo().GetName(),
			Metadata:   result.GetInfo().GetMetadata(),
			MapPinPath: result.GetInfo().GetMapPinPath(),
		})
	}

	return programs, nil
}

/*
Close closes the client connection to bpfman.
*/
func (c *client) Close() error {
	return c.conn.Close()
}
