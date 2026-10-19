/*
 * Copyright(c) 2022 Intel Corporation.
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

package deviceplugin

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/redhat-et/afxdp-plugins-for-kubernetes/constants"
	"github.com/redhat-et/afxdp-plugins-for-kubernetes/internal/bpf"
	"github.com/redhat-et/afxdp-plugins-for-kubernetes/internal/bpfman"
	"github.com/redhat-et/afxdp-plugins-for-kubernetes/internal/networking"
	"github.com/redhat-et/afxdp-plugins-for-kubernetes/internal/udsserver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

func TestAllocate(t *testing.T) {
	netHandler := networking.NewFakeHandler()

	config := PoolConfig{
		Name: "myPool",
		Mode: "primary",
		Devices: map[string]*networking.Device{
			"dev_1": networking.CreateTestDevice("dev_1", "primary", "ice", "0000:81:00.1", "68:05:ca:2d:e9:01", netHandler),
			"dev_2": networking.CreateTestDevice("dev_2", "primary", "ice", "0000:81:00.2", "68:05:ca:2d:e9:02", netHandler),
			"dev_3": networking.CreateTestDevice("dev_3", "primary", "ice", "0000:81:00.3", "68:05:ca:2d:e9:03", netHandler),
			"dev_4": networking.CreateTestDevice("dev_4", "primary", "ice", "0000:81:00.4", "68:05:ca:2d:e9:04", netHandler),
			"dev_5": networking.CreateTestDevice("dev_5", "primary", "ice", "0000:81:00.5", "68:05:ca:2d:e9:05", netHandler),
			"dev_6": networking.CreateTestDevice("dev_6", "primary", "ice", "0000:81:00.6", "68:05:ca:2d:e9:06", netHandler),
			"dev_7": networking.CreateTestDevice("dev_7", "primary", "ice", "0000:81:00.7", "68:05:ca:2d:e9:07", netHandler),
			"dev_8": networking.CreateTestDevice("dev_8", "primary", "ice", "0000:81:00.8", "68:05:ca:2d:e9:08", netHandler),
			"dev_9": networking.CreateTestDevice("dev_9", "primary", "ice", "0000:81:00.9", "68:05:ca:2d:e9:09", netHandler),
		},
		UdsServerDisable:        false,
		BpfMapPinningEnable:     false,
		UdsTimeout:              0,
		UdsFuzz:                 false,
		RequiresUnprivilegedBpf: false,
		UID:                     1500,
	}

	pm := NewPoolManager(config)
	pm.ServerFactory = udsserver.NewFakeServerFactory()
	pm.BpfHandler = bpf.NewFakeHandler()

	envVar := constants.Devices.EnvVarList + strings.ToUpper(pm.Name)

	testCases := []struct {
		name                  string
		containerRequests     []*pluginapi.ContainerAllocateRequest
		expContainerResponses []*pluginapi.ContainerAllocateResponse
	}{
		{
			name: "Single Container Single Device",
			containerRequests: []*pluginapi.ContainerAllocateRequest{
				{DevicesIDs: []string{"dev_1"}},
			},
			expContainerResponses: []*pluginapi.ContainerAllocateResponse{
				{
					Envs: map[string]string{envVar: "dev_1"},
					Mounts: []*pluginapi.Mount{
						{
							ContainerPath: constants.Uds.PodPath + "dev_1" + constants.Uds.SockName,
							HostPath:      "/tmp/fake-socket.sock",
							ReadOnly:      false,
						},
					},
					Devices:     []*pluginapi.DeviceSpec{},
					Annotations: map[string]string{},
				},
			},
		},

		{
			name: "Single Container Multiple Devices",
			containerRequests: []*pluginapi.ContainerAllocateRequest{
				{DevicesIDs: []string{"dev_1", "dev_2", "dev_3"}},
			},
			expContainerResponses: []*pluginapi.ContainerAllocateResponse{
				{
					Envs: map[string]string{envVar: "dev_1 dev_2 dev_3"},
					Mounts: []*pluginapi.Mount{
						{
							ContainerPath: constants.Uds.PodPath + "dev_1" + constants.Uds.SockName,
							HostPath:      "/tmp/fake-socket.sock",
							ReadOnly:      false,
						},
						{
							ContainerPath: constants.Uds.PodPath + "dev_2" + constants.Uds.SockName,
							HostPath:      "/tmp/fake-socket.sock",
							ReadOnly:      false,
						},
						{
							ContainerPath: constants.Uds.PodPath + "dev_3" + constants.Uds.SockName,
							HostPath:      "/tmp/fake-socket.sock",
							ReadOnly:      false,
						},
					},
					Devices:     []*pluginapi.DeviceSpec{},
					Annotations: map[string]string{},
				},
			},
		},

		{
			name: "Multiple Containers Single Device",
			containerRequests: []*pluginapi.ContainerAllocateRequest{
				{DevicesIDs: []string{"dev_1"}},
				{DevicesIDs: []string{"dev_2"}},
			},
			expContainerResponses: []*pluginapi.ContainerAllocateResponse{
				{
					Envs: map[string]string{envVar: "dev_1"},
					Mounts: []*pluginapi.Mount{
						{
							ContainerPath: constants.Uds.PodPath + "dev_1" + constants.Uds.SockName,
							HostPath:      "/tmp/fake-socket.sock",
							ReadOnly:      false,
						},
					},
					Devices:     []*pluginapi.DeviceSpec{},
					Annotations: map[string]string{},
				},
				{
					Envs: map[string]string{envVar: "dev_2"},
					Mounts: []*pluginapi.Mount{
						{
							ContainerPath: constants.Uds.PodPath + "dev_2" + constants.Uds.SockName,
							HostPath:      "/tmp/fake-socket.sock",
							ReadOnly:      false,
						},
					},
					Devices:     []*pluginapi.DeviceSpec{},
					Annotations: map[string]string{},
				},
			},
		},

		{
			name: "Multiple Containers Multiple Devices",
			containerRequests: []*pluginapi.ContainerAllocateRequest{
				{DevicesIDs: []string{"dev_1", "dev_2", "dev_3"}},
				{DevicesIDs: []string{"dev_4", "dev_5", "dev_6"}},
			},
			expContainerResponses: []*pluginapi.ContainerAllocateResponse{
				{
					Envs: map[string]string{envVar: "dev_1 dev_2 dev_3"},
					Mounts: []*pluginapi.Mount{
						{
							ContainerPath: constants.Uds.PodPath + "dev_1" + constants.Uds.SockName,
							HostPath:      "/tmp/fake-socket.sock",
							ReadOnly:      false,
						},
						{
							ContainerPath: constants.Uds.PodPath + "dev_2" + constants.Uds.SockName,
							HostPath:      "/tmp/fake-socket.sock",
							ReadOnly:      false,
						},
						{
							ContainerPath: constants.Uds.PodPath + "dev_3" + constants.Uds.SockName,
							HostPath:      "/tmp/fake-socket.sock",
							ReadOnly:      false,
						},
					},
					Devices:     []*pluginapi.DeviceSpec{},
					Annotations: map[string]string{},
				},
				{
					Envs: map[string]string{envVar: "dev_4 dev_5 dev_6"},
					Mounts: []*pluginapi.Mount{
						{
							ContainerPath: constants.Uds.PodPath + "dev_4" + constants.Uds.SockName,
							HostPath:      "/tmp/fake-socket.sock",
							ReadOnly:      false,
						},
						{
							ContainerPath: constants.Uds.PodPath + "dev_5" + constants.Uds.SockName,
							HostPath:      "/tmp/fake-socket.sock",
							ReadOnly:      false,
						},
						{
							ContainerPath: constants.Uds.PodPath + "dev_6" + constants.Uds.SockName,
							HostPath:      "/tmp/fake-socket.sock",
							ReadOnly:      false,
						},
					},
					Devices:     []*pluginapi.DeviceSpec{},
					Annotations: map[string]string{},
				},
			},
		},

		{
			name: "No Device",
			containerRequests: []*pluginapi.ContainerAllocateRequest{
				{DevicesIDs: []string{}},
			},
			expContainerResponses: []*pluginapi.ContainerAllocateResponse{
				{
					Envs:        map[string]string{envVar: ""},
					Mounts:      []*pluginapi.Mount{},
					Devices:     []*pluginapi.DeviceSpec{},
					Annotations: map[string]string{},
				},
			},
		},
		{
			name:                  "No Container",
			containerRequests:     []*pluginapi.ContainerAllocateRequest{},
			expContainerResponses: []*pluginapi.ContainerAllocateResponse{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {

			allocateRequest := &pluginapi.AllocateRequest{
				ContainerRequests: tc.containerRequests,
			}

			expectedResponse := &pluginapi.AllocateResponse{
				ContainerResponses: tc.expContainerResponses,
			}

			response, err := pm.Allocate(context.Background(), allocateRequest)

			if err != nil {
				assert.FailNow(t, "Unexpected error during Allocate %v", err)
			}

			//TODO error
			expectedJSON, _ := json.Marshal(expectedResponse)
			responseJSON, _ := json.Marshal(response)

			assert.Equal(t, string(expectedJSON), string(responseJSON), "Unexpected AllocateResponse")

		})
	}
}

func TestListAndWatchSendsInitialDeviceList(t *testing.T) {
	netHandler := networking.NewFakeHandler()

	config := PoolConfig{
		Name: "myPool",
		Mode: "primary",
		Devices: map[string]*networking.Device{
			"dev_1": networking.CreateTestDevice("dev_1", "primary", "ice", "0000:81:00.1", "68:05:ca:2d:e9:01", netHandler),
			"dev_2": networking.CreateTestDevice("dev_2", "primary", "ice", "0000:81:00.2", "68:05:ca:2d:e9:02", netHandler),
		},
	}

	pm := NewPoolManager(config)

	// fake ListAndWatch stream: collects responses and ends the stream
	// (with an error) after two sends so the ListAndWatch goroutine returns
	fake := &fakeListAndWatchServer{done: make(chan struct{})}
	go func() {
		_ = pm.ListAndWatch(&pluginapi.Empty{}, fake)
	}()

	// trigger the update response; the channel is unbuffered so this is
	// received only after the initial list has been sent
	pm.UpdateSignal <- true

	select {
	case <-fake.done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for ListAndWatch responses")
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	require.Len(t, fake.responses, 2, "expected the initial list plus one update")

	ids := func(resp *pluginapi.ListAndWatchResponse) []string {
		var out []string
		for _, dev := range resp.GetDevices() {
			out = append(out, dev.GetID())
		}
		return out
	}
	assert.ElementsMatch(t, []string{"dev_1", "dev_2"}, ids(fake.responses[0]),
		"the initial response must list the pool's devices")
	assert.ElementsMatch(t, []string{"dev_1", "dev_2"}, ids(fake.responses[1]),
		"the update response must list the pool's devices")
}

// fakeListAndWatchServer implements the parts of the
// DevicePlugin_ListAndWatchServer interface that ListAndWatch uses. It ends
// the stream (with an error) after collecting two responses so the
// ListAndWatch goroutine terminates.
type fakeListAndWatchServer struct {
	pluginapi.DevicePlugin_ListAndWatchServer

	mu        sync.Mutex
	responses []*pluginapi.ListAndWatchResponse
	done      chan struct{}
}

func (f *fakeListAndWatchServer) Send(resp *pluginapi.ListAndWatchResponse) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.responses = append(f.responses, resp)

	if len(f.responses) >= 2 {
		select {
		case <-f.done:
		default:
			close(f.done)
		}
		return errors.New("end of test stream")
	}
	return nil
}

func TestAllocateBpfman(t *testing.T) {
	netHandler := networking.NewFakeHandler()

	config := PoolConfig{
		Name: "myPool",
		Mode: "primary",
		Devices: map[string]*networking.Device{
			"dev_1": networking.CreateTestDevice("dev_1", "primary", "ice", "0000:81:00.1", "68:05:ca:2d:e9:01", netHandler),
			"dev_2": networking.CreateTestDevice("dev_2", "primary", "ice", "0000:81:00.2", "68:05:ca:2d:e9:02", netHandler),
		},
		UdsServerDisable:    false,
		BpfMapPinningEnable: false,
		BpfmanEnable:        true,
		BpfmanBytecodeFile:  "/afxdp/xdp_afxdp_redirect_bpfman.o",
		BpfmanFunction:      "xdp_afxdp_redirect",
		BpfmanPriority:      10,
		UID:                 1500,
	}

	pm := NewPoolManager(config)
	pm.BpfmanHandler = bpfman.NewFakeHandler()

	envVar := constants.Devices.EnvVarList + strings.ToUpper(pm.Name)

	allocateRequest := &pluginapi.AllocateRequest{
		ContainerRequests: []*pluginapi.ContainerAllocateRequest{
			{DevicesIDs: []string{"dev_1"}},
		},
	}

	response, err := pm.Allocate(context.Background(), allocateRequest)
	require.NoError(t, err)
	require.Len(t, response.ContainerResponses, 1)

	cresp := response.ContainerResponses[0]
	assert.Equal(t, map[string]string{envVar: "dev_1"}, cresp.Envs)
	assert.Empty(t, cresp.Mounts, "bpfman mode should not mount UDS sockets or pinned maps")

	// the fake bpfman handler should not be used during Allocate; programs
	// follow the pool lifecycle, not the pod lifecycle
	fake := pm.BpfmanHandler.(*bpfman.FakeHandler)
	assert.Empty(t, fake.Programs, "Allocate should not load bpfman programs")
}

func TestInitBpfman(t *testing.T) {
	netHandler := networking.NewFakeHandler()

	newConfig := func() PoolConfig {
		return PoolConfig{
			Name: "myPool",
			Mode: "primary",
			Devices: map[string]*networking.Device{
				"dev_1": networking.CreateTestDevice("dev_1", "primary", "ice", "0000:81:00.1", "68:05:ca:2d:e9:01", netHandler),
				"dev_2": networking.CreateTestDevice("dev_2", "primary", "ice", "0000:81:00.2", "68:05:ca:2d:e9:02", netHandler),
			},
			BpfmanEnable:       true,
			BpfmanBytecodeFile: "/afxdp/xdp_afxdp_redirect_bpfman.o",
			BpfmanFunction:     "xdp_afxdp_redirect",
			BpfmanPriority:     10,
		}
	}

	t.Run("loads a program per device", func(t *testing.T) {
		pm := NewPoolManager(newConfig())
		pm.BpfmanHandler = bpfman.NewFakeHandler()

		require.NoError(t, pm.initBpfman())

		fake := pm.BpfmanHandler.(*bpfman.FakeHandler)
		assert.Len(t, fake.Programs, 2)
		for devName, id := range pm.bpfmanPrograms {
			assert.NotZero(t, id)
			opts := fake.Programs[id]
			require.NotNil(t, opts)
			assert.Equal(t, devName, opts.Iface)
			assert.Equal(t, "afxdp-myPool-"+devName, opts.ProgramName)
			assert.Equal(t, int32(10), opts.Priority)
			assert.Equal(t, "myPool", opts.ExtraMetadata[bpfman.MetadataKeyPool])
			assert.Equal(t, devName, opts.ExtraMetadata[bpfman.MetadataKeyDevice])
		}
	})

	t.Run("load error fails pool init", func(t *testing.T) {
		pm := NewPoolManager(newConfig())
		fake := bpfman.NewFakeHandler()
		fake.LoadError = errors.New("bpfman unavailable")
		pm.BpfmanHandler = fake

		err := pm.initBpfman()
		require.Error(t, err)
	})

	t.Run("adopts existing programs and unloads orphans", func(t *testing.T) {
		pm := NewPoolManager(newConfig())
		fake := bpfman.NewFakeHandler()

		// program for dev_1 left over from a previous run
		existing, err := fake.LoadAndAttachXDP(context.Background(), bpfman.XDPLoadOptions{
			BytecodeFile:  "/afxdp/xdp_afxdp_redirect_bpfman.o",
			FunctionName:  "xdp_afxdp_redirect",
			ProgramName:   "afxdp-myPool-dev_1",
			Iface:         "dev_1",
			ExtraMetadata: map[string]string{bpfman.MetadataKeyPool: "myPool", bpfman.MetadataKeyDevice: "dev_1"},
		})
		require.NoError(t, err)

		// orphaned program for a device no longer in the pool
		orphan, err := fake.LoadAndAttachXDP(context.Background(), bpfman.XDPLoadOptions{
			BytecodeFile:  "/afxdp/xdp_afxdp_redirect_bpfman.o",
			FunctionName:  "xdp_afxdp_redirect",
			ProgramName:   "afxdp-myPool-dev_gone",
			Iface:         "dev_gone",
			ExtraMetadata: map[string]string{bpfman.MetadataKeyPool: "myPool", bpfman.MetadataKeyDevice: "dev_gone"},
		})
		require.NoError(t, err)

		pm.BpfmanHandler = fake
		require.NoError(t, pm.initBpfman())

		assert.Equal(t, existing.ID, pm.bpfmanPrograms["dev_1"], "existing program should be adopted")
		require.Contains(t, pm.bpfmanPrograms, "dev_2", "missing program should be loaded")

		_, stillLoaded := fake.Programs[orphan.ID]
		assert.False(t, stillLoaded, "orphaned program should be unloaded")

		assert.Len(t, fake.Programs, 2)
	})
}

func TestTerminateBpfman(t *testing.T) {
	netHandler := networking.NewFakeHandler()

	config := PoolConfig{
		Name: "myPool",
		Mode: "primary",
		Devices: map[string]*networking.Device{
			"dev_1": networking.CreateTestDevice("dev_1", "primary", "ice", "0000:81:00.1", "68:05:ca:2d:e9:01", netHandler),
		},
		BpfmanEnable:       true,
		BpfmanBytecodeFile: "/afxdp/xdp_afxdp_redirect_bpfman.o",
		BpfmanFunction:     "xdp_afxdp_redirect",
		BpfmanPriority:     10,
	}

	pm := NewPoolManager(config)
	fake := bpfman.NewFakeHandler()
	pm.BpfmanHandler = fake

	require.NoError(t, pm.initBpfman())
	require.Len(t, fake.Programs, 1)

	require.NoError(t, pm.Terminate())

	assert.Empty(t, fake.Programs, "pool programs should be unloaded on terminate")
	assert.True(t, fake.Closed, "bpfman client should be closed on terminate")
}
