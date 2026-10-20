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
	"fmt"
	"sync"
)

/*
FakeHandler is a fake implementation of the Handler interface for testing.
It records calls and simulates a bpfman daemon in memory.
*/
type FakeHandler struct {
	mu sync.Mutex
	// NextID is the kernel program id assigned to the next loaded program.
	NextID uint32
	// Programs maps program id to the options it was loaded with.
	Programs map[uint32]*XDPLoadOptions
	// AttachedIfaces records the ifaces programs were attached to, keyed by program id.
	AttachedIfaces map[uint32]string
	// LoadError, if set, is returned by LoadAndAttachXDP.
	LoadError error
	// UnloadError, if set, is returned by Unload.
	UnloadError error
	// ListError, if set, is returned by ListXDPPrograms.
	ListError error
	// LoadErrorForIface, if set for an interface name, is returned by LoadAndAttachXDP for that iface.
	LoadErrorForIface map[string]error
	// Closed records whether Close was called.
	Closed bool
}

/*
NewFakeHandler returns a FakeHandler primed with a non-zero starting id.
*/
func NewFakeHandler() *FakeHandler {
	return &FakeHandler{
		NextID:         100,
		Programs:       make(map[uint32]*XDPLoadOptions),
		AttachedIfaces: make(map[uint32]string),
	}
}

/*
LoadAndAttachXDP simulates loading and attaching an XDP program.
*/
func (f *FakeHandler) LoadAndAttachXDP(_ context.Context, opts XDPLoadOptions) (*XDPProgram, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.LoadError != nil {
		return nil, f.LoadError
	}
	if f.LoadErrorForIface != nil {
		if err := f.LoadErrorForIface[opts.Iface]; err != nil {
			return nil, err
		}
	}

	f.NextID++
	id := f.NextID
	stored := opts
	f.Programs[id] = &stored
	f.AttachedIfaces[id] = opts.Iface

	return &XDPProgram{
		ID:         id,
		LinkID:     id,
		MapPinPath: fmt.Sprintf("/run/bpfman/fs/maps/%d", id),
	}, nil
}

/*
Unload simulates unloading a program.
*/
func (f *FakeHandler) Unload(_ context.Context, id uint32) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.UnloadError != nil {
		return f.UnloadError
	}

	if _, ok := f.Programs[id]; !ok {
		return fmt.Errorf("program %d not found", id)
	}
	delete(f.Programs, id)
	delete(f.AttachedIfaces, id)

	return nil
}

/*
ListXDPPrograms simulates listing programs, filtered on exact metadata matches.
*/
func (f *FakeHandler) ListXDPPrograms(_ context.Context, matchMetadata map[string]string) ([]ProgramSummary, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.ListError != nil {
		return nil, f.ListError
	}

	metadataFor := func(opts *XDPLoadOptions) map[string]string {
		m := map[string]string{
			MetadataKeyProgramName: opts.ProgramName,
			MetadataKeyOwner:       OwnerValue,
		}
		for k, v := range ConfigMetadata(*opts) {
			m[k] = v
		}
		for k, v := range opts.ExtraMetadata {
			m[k] = v
		}
		return m
	}

	var programs []ProgramSummary
	for id, opts := range f.Programs {
		metadata := metadataFor(opts)
		matched := true
		for k, v := range matchMetadata {
			if metadata[k] != v {
				matched = false
				break
			}
		}
		if !matched {
			continue
		}
		programs = append(programs, ProgramSummary{
			ID:         id,
			Name:       opts.FunctionName,
			Metadata:   metadata,
			MapPinPath: fmt.Sprintf("/run/bpfman/fs/maps/%d", id),
		})
	}

	return programs, nil
}

/*
Close simulates closing the connection.
*/
func (f *FakeHandler) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.Closed = true
	return nil
}
