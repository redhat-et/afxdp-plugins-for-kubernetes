/*
 * Copyright(c) 2022 Intel Corporation.
 * Copyright(c) Red Hat Inc.
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *	 http://www.apache.org/licenses/LICENSE-2.0
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
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/redhat-et/afxdp-plugins-for-kubernetes/constants"
	"github.com/redhat-et/afxdp-plugins-for-kubernetes/internal/bpf"
	"github.com/redhat-et/afxdp-plugins-for-kubernetes/internal/bpfman"
	"github.com/redhat-et/afxdp-plugins-for-kubernetes/internal/dpcnisyncerserver"
	"github.com/redhat-et/afxdp-plugins-for-kubernetes/internal/networking"
	"github.com/redhat-et/afxdp-plugins-for-kubernetes/internal/tools"
	"github.com/redhat-et/afxdp-plugins-for-kubernetes/internal/udsserver"
	logging "github.com/sirupsen/logrus"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

/*
PoolManager represents an manages the pool of devices.
Each PoolManager registers with Kubernetes as a different device type.
*/
type PoolManager struct {
	Name                string
	Mode                string
	Devices             map[string]*networking.Device
	UpdateSignal        chan bool
	DpAPISocket         string
	DpAPIEndpoint       string
	UdsServerDisable    bool
	BpfMapPinningEnable bool
	UdsTimeout          int
	DevicePrefix        string
	UdsFuzz             bool
	UID                 string
	EthtoolFilters      []string
	DpAPIServer         *grpc.Server
	ServerFactory       udsserver.ServerFactory
	MapManagerFactory   bpf.MapManagerFactory
	BpfHandler          bpf.Handler
	NetHandler          networking.Handler
	DpCniSyncerServer   *dpcnisyncerserver.SyncerServer
	DpCniSyncerSocket   string
	SyncerActive        bool
	Pbm                 bpf.PoolBpfMapManager
	BpfmanEnable        bool
	BpfmanBytecodeFile  string
	BpfmanBytecodeImage string
	BpfmanFunction      string
	BpfmanPriority      int32
	BpfmanSocket        string
	BpfmanHandler       bpfman.Handler
	bpfmanPrograms      map[string]uint32
	stopWatcher         chan struct{}
}

func NewPoolManager(config PoolConfig) PoolManager {
	return PoolManager{
		Name:                config.Name,
		Mode:                config.Mode,
		Devices:             config.Devices,
		UpdateSignal:        make(chan bool),
		DpAPISocket:         pluginapi.DevicePluginPath + constants.Plugins.DevicePlugin.DevicePrefix + "-" + config.Name + ".sock",
		DpAPIEndpoint:       constants.Plugins.DevicePlugin.DevicePrefix + "-" + config.Name + ".sock",
		UdsServerDisable:    config.UdsServerDisable,
		BpfMapPinningEnable: config.BpfMapPinningEnable,
		UdsTimeout:          config.UdsTimeout,
		DevicePrefix:        constants.Plugins.DevicePlugin.DevicePrefix,
		UdsFuzz:             config.UdsFuzz,
		UID:                 strconv.Itoa(config.UID),
		EthtoolFilters:      config.EthtoolCmds,
		DpCniSyncerServer:   config.DPCNIServer,
		BpfmanEnable:        config.BpfmanEnable,
		BpfmanBytecodeFile:  config.BpfmanBytecodeFile,
		BpfmanBytecodeImage: config.BpfmanBytecodeImage,
		BpfmanFunction:      config.BpfmanFunction,
		BpfmanPriority:      config.BpfmanPriority,
		BpfmanSocket:        config.BpfmanSocket,
		bpfmanPrograms:      make(map[string]uint32),
		stopWatcher:         make(chan struct{}),
	}
}

/*
Init is called it initialise the PoolManager.
*/
func (pm *PoolManager) Init(config PoolConfig) error {
	pm.ServerFactory = udsserver.NewServerFactory()
	pm.MapManagerFactory = bpf.NewMapMangerFactory()
	pm.BpfHandler = bpf.NewHandler()
	pm.NetHandler = networking.NewHandler()

	if err := pm.startGRPC(); err != nil {
		return err
	}
	logging.Infof("Pool "+pm.DevicePrefix+"/%s started serving", pm.Name)

	// Load and attach the pool's XDP programs via bpfman before the pool is
	// registered with Kubelet, so no pod can be scheduled onto a device
	// before its program (and pinned XSKMAP) exists.
	if pm.BpfmanEnable {
		if err := pm.initBpfman(); err != nil {
			return err
		}
	}

	if err := pm.registerWithKubelet(); err != nil {
		return err
	}
	logging.Infof("Pool "+pm.DevicePrefix+"/%s registered with Kubelet", pm.Name)

	// keep the pool registered across kubelet restarts
	go pm.watchSocket()

	if pm.BpfMapPinningEnable {
		var err error

		logging.Infof("Creating new BPF Map manager %s %s", pm.Name, pm.UID)
		pm.Pbm.Manager, err = pm.MapManagerFactory.CreateMapManager(pm.Name, pm.UID)
		if err != nil {
			logging.Errorf("Error new BPF Map manager: %v", err)
			return err
		}

		logging.Debug("REGISTER MAP MANAGER WITH THE DP<=>CNI grpc Syncer")
		pm.DpCniSyncerServer.RegisterMapManager(pm.Pbm)
		pm.DpCniSyncerServer.BpfMapPinEnable = true
	}

	// no initial UpdateSignal is needed here: ListAndWatch sends the
	// current device list as soon as kubelet connects

	return nil
}

/*
Terminate is called it terminate the PoolManager.
*/
func (pm *PoolManager) Terminate() error {
	// stop the socket watcher before tearing down the gRPC server, so it
	// does not mistake our own cleanup for a kubelet restart
	if pm.stopWatcher != nil {
		select {
		case <-pm.stopWatcher:
			// already closed
		default:
			close(pm.stopWatcher)
		}
	}
	pm.stopGRPC()
	if err := pm.cleanup(); err != nil {
		logging.Errorf("Cleanup error: %v", err)
	}
	logging.Infof("%s/%s terminated", pm.DevicePrefix, pm.Name)

	if pm.DpCniSyncerServer != nil {
		pm.DpCniSyncerServer.StopGRPCSyncer()
	}

	if pm.BpfMapPinningEnable {
		if err := pm.Pbm.Manager.CleanupMapManager(); err != nil {
			logging.Errorf("Cleanup error: %v", err)
		}
	}

	if pm.BpfmanEnable {
		pm.terminateBpfman()
	}

	return nil
}

/*
initBpfman loads and attaches the pool's XDP program on every device in the
pool via bpfman.

Programs follow the pool lifecycle rather than the pod lifecycle: they are
loaded before the pool is registered with Kubelet and unloaded when the pool
terminates. This guarantees the pinned XSKMAP exists before any pod's CSI
volume mount and that idle pooled devices simply pass traffic to the stack
(the redirect program falls back to XDP_PASS when no AF_XDP socket is
inserted into the map).
*/
func (pm *PoolManager) initBpfman() error {
	ctx := context.Background()

	if pm.BpfmanHandler == nil {
		handler, err := bpfman.NewHandler(pm.BpfmanSocket)
		if err != nil {
			return err
		}
		pm.BpfmanHandler = handler
	}

	// In cdq mode the subfunctions must exist on the host before their XDP
	// programs can be attached, so activate them eagerly rather than at
	// Allocate time.
	if pm.Mode == "cdq" {
		for _, device := range pm.Devices {
			if err := device.ActivateCdqSubfunction(); err != nil {
				logging.Errorf("Error activating CDQ subfunction %s: %v", device.Name(), err)
				return err
			}
		}
	}

	// Reconcile with programs left behind from a previous run: adopt
	// programs whose device is still in this pool, unload the rest.
	programs, err := pm.BpfmanHandler.ListXDPPrograms(ctx, map[string]string{
		bpfman.MetadataKeyOwner: bpfman.OwnerValue,
		bpfman.MetadataKeyPool:  pm.Name,
	})
	if err != nil {
		return err
	}

	for _, program := range programs {
		device := program.Metadata[bpfman.MetadataKeyDevice]
		if _, ok := pm.Devices[device]; ok {
			logging.Infof("Adopting existing bpfman program %d for device %s", program.ID, device)
			pm.bpfmanPrograms[device] = program.ID
		} else {
			logging.Infof("Unloading orphaned bpfman program %d for device %s", program.ID, device)
			if err := pm.BpfmanHandler.Unload(ctx, program.ID); err != nil {
				logging.Errorf("Error unloading orphaned bpfman program %d: %v", program.ID, err)
			}
		}
	}

	// Load and attach the program on every device that does not have one.
	for deviceName := range pm.Devices {
		if pm.bpfmanPrograms[deviceName] != 0 {
			continue
		}

		logging.Infof("Loading bpfman XDP program on device: %s", deviceName)
		program, err := pm.BpfmanHandler.LoadAndAttachXDP(ctx, pm.bpfmanLoadOptions(deviceName))
		if err != nil {
			logging.Errorf("Error loading bpfman XDP program on device %s: %v", deviceName, err)
			return err
		}
		logging.Infof("bpfman XDP program loaded on: %s, program ID: %d, map pin path: %s",
			deviceName, program.ID, program.MapPinPath)

		pm.bpfmanPrograms[deviceName] = program.ID
	}

	return nil
}

/*
terminateBpfman unloads all of the pool's bpfman programs and deactivates
CDQ subfunctions that were eagerly activated for the pool.
*/
func (pm *PoolManager) terminateBpfman() {
	ctx := context.Background()

	for deviceName, id := range pm.bpfmanPrograms {
		if err := pm.BpfmanHandler.Unload(ctx, id); err != nil {
			logging.Errorf("Error unloading bpfman program %d for device %s: %v", id, deviceName, err)
		} else {
			logging.Infof("bpfman program %d unloaded from device %s", id, deviceName)
		}
	}

	// In bpfman mode the CNI does not delete CDQ subfunctions at pod
	// teardown, so clean them up here.
	if pm.Mode == "cdq" {
		for _, device := range pm.Devices {
			if isSf, err := pm.NetHandler.IsCdqSubfunction(device.Name()); err != nil {
				logging.Errorf("Error determining if %s is a CDQ subfunction: %v", device.Name(), err)
			} else if isSf {
				portIndex, err := pm.NetHandler.GetCdqPortIndex(device.Name())
				if err != nil {
					logging.Errorf("Error getting port index of device %s: %v", device.Name(), err)
					continue
				}
				if err := pm.NetHandler.DeleteCdqSubfunction(portIndex); err != nil {
					logging.Errorf("Error deleting CDQ subfunction %s: %v", device.Name(), err)
				}
			}
		}
	}

	if pm.BpfmanHandler != nil {
		if err := pm.BpfmanHandler.Close(); err != nil {
			logging.Errorf("Error closing bpfman client: %v", err)
		}
	}
}

/*
bpfmanLoadOptions builds the bpfman load options for a device in this pool.
The program name is deterministic (afxdp-<pool>-<netdev>) so that pods can
reference it in the csi.bpfman.io/program volume attribute.
*/
func (pm *PoolManager) bpfmanLoadOptions(deviceName string) bpfman.XDPLoadOptions {
	programName := constants.Bpfman.ProgramNamePrefix + "-" + pm.Name + "-" + deviceName

	return bpfman.XDPLoadOptions{
		BytecodeFile:  pm.BpfmanBytecodeFile,
		BytecodeImage: pm.BpfmanBytecodeImage,
		FunctionName:  pm.BpfmanFunction,
		ProgramName:   programName,
		Iface:         deviceName,
		Priority:      pm.BpfmanPriority,
		ExtraMetadata: map[string]string{
			bpfman.MetadataKeyPool:   pm.Name,
			bpfman.MetadataKeyDevice: deviceName,
		},
	}
}

/*
ListAndWatch is part of the device plugin API.
Returns a stream list of Devices. Whenever a device state changes,
ListAndWatch should return the new list.
*/
func (pm *PoolManager) ListAndWatch(empty *pluginapi.Empty,
	stream pluginapi.DevicePlugin_ListAndWatchServer) error {

	logging.Debugf("Pool "+pm.DevicePrefix+"/%s ListAndWatch started", pm.Name)

	// send the current device list immediately, so a (re)connecting Kubelet
	// advertises the pool's capacity without waiting for a device update.
	// Kubelet opens a new ListAndWatch stream whenever the plugin
	// (re-)registers with it.
	if err := stream.Send(pm.deviceListResponse()); err != nil {
		logging.Errorf("Failed to send initial device list to kubelet: %v", err)
		return err
	}

	for {
		<-pm.UpdateSignal
		if err := stream.Send(pm.deviceListResponse()); err != nil {
			logging.Errorf("Failed to send stream to kubelet: %v", err)
			return err
		}
	}
}

func (pm *PoolManager) deviceListResponse() *pluginapi.ListAndWatchResponse {
	resp := new(pluginapi.ListAndWatchResponse)

	for devName := range pm.Devices {
		resp.Devices = append(resp.Devices, &pluginapi.Device{ID: devName, Health: pluginapi.Healthy})
	}

	return resp
}

/*
watchSocket monitors this pool's device plugin socket and re-registers the
pool with Kubelet when the socket disappears.

Kubelet deletes the sockets in the device plugin directory whenever it
restarts, so a plugin that only registered once at startup becomes invisible
to the new Kubelet: the pool's resource capacity drops to zero and pods
requesting the resource fail admission. Watching for the deleted socket and
re-registering is the recommended behaviour in the Kubernetes device plugin
documentation.
*/
func (pm *PoolManager) watchSocket() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	needRegister := false

	for {
		select {
		case <-pm.stopWatcher:
			return
		case <-ticker.C:
		}

		if _, err := os.Stat(pm.DpAPISocket); err != nil {
			logging.Warningf("Pool "+pm.DevicePrefix+"/%s: device plugin socket %s disappeared (kubelet restart?) - re-registering", pm.Name, pm.DpAPISocket)

			pm.stopGRPC()
			if err := pm.startGRPC(); err != nil {
				logging.Errorf("Pool "+pm.DevicePrefix+"/%s: error restarting the gRPC server: %v", pm.Name, err)
				continue
			}
			needRegister = true
		}

		if !needRegister {
			continue
		}

		// kubelet may still be starting up: registration is retried on
		// each tick until it succeeds
		if err := pm.registerWithKubelet(); err != nil {
			logging.Errorf("Pool "+pm.DevicePrefix+"/%s: error re-registering with Kubelet: %v", pm.Name, err)
			continue
		}
		needRegister = false
		logging.Infof("Pool "+pm.DevicePrefix+"/%s re-registered with Kubelet", pm.Name)
	}
}

/*
Allocate is part of the device plugin API.
Called during container creation so that the Device Plugin can run
device specific operations and instruct Kubelet of the steps to make
the Device available in the container.
*/
func (pm *PoolManager) Allocate(ctx context.Context,
	rqt *pluginapi.AllocateRequest) (*pluginapi.AllocateResponse, error) {
	response := pluginapi.AllocateResponse{}
	var udsServer udsserver.Server
	var udsPath string
	var err error

	logging.Debugf("New allocate request on pool %s", pm.Name)

	// In bpfman mode the XDP program and its pinned XSKMAP follow the pool
	// lifecycle; there is no per-pod UDS server or map pinning.
	udsEnabled := !pm.UdsServerDisable && !pm.BpfmanEnable

	if udsEnabled {
		logging.Infof("Creating new UDS server")
		udsServer, udsPath, err = pm.ServerFactory.CreateServer(pm.DevicePrefix+"/"+pm.Name, pm.UID, pm.UdsTimeout, pm.UdsFuzz)
		if err != nil {
			logging.Errorf("Error Creating new UDS server: %v", err)
			return &response, err
		}
	}

	//loop each container request
	for _, crqt := range rqt.ContainerRequests {
		cresp := new(pluginapi.ContainerAllocateResponse)
		envs := make(map[string]string)

		//loop each device request per container
		for _, devName := range crqt.DevicesIDs {
			device := pm.Devices[devName]
			pretty, _ := tools.PrettyString(device.Public())
			logging.Debugf("Device: %s", pretty)

			containerSockPath := constants.Uds.PodPath + device.Name() + constants.Uds.SockName

			if udsEnabled {
				cresp.Mounts = append(cresp.Mounts, &pluginapi.Mount{
					HostPath:      udsPath,
					ContainerPath: containerSockPath,
					ReadOnly:      false,
				})
			}

			if device.Mode() != pm.Mode {
				err := fmt.Errorf("pool mode %s does not match device mode %s", pm.Mode, device.Mode())
				logging.Errorf("%v", err)
				return &response, err
			}

			switch pm.Mode {
			case "primary":
				logging.Debugf("Primary mode")
			case "cdq":
				if err := device.ActivateCdqSubfunction(); err != nil {
					logging.Errorf("Error creating CDQ subfunction: %v", err)
					return &response, err
				}
			default:
				err := fmt.Errorf("unsupported pool mode: %s", pm.Mode)
				logging.Errorf("%v", err)
				return &response, err
			}

			logging.Debugf("Cycling state of device %s", device.Name())
			if !pm.BpfmanEnable {
				// The link cycle is a workaround for XDP load errors (-22) seen
				// when the plugin itself loads the program at Allocate time. In
				// bpfman mode the program is already attached for the lifetime
				// of the pool so the cycle is unnecessary.
				if err := device.Cycle(); err != nil {
					logging.Errorf("Error cycling the state of device %s: %v", device.Name(), err)
					continue
				}
			}

			if udsEnabled {
				logging.Infof("Loading BPF program on device: %s", device.Name())
				fd, err := pm.BpfHandler.LoadBpfSendXskMap(device.Name())
				if err != nil {
					logging.Errorf("Error loading BPF Program on interface %s: %v", device.Name(), err)
					return &response, err
				}
				logging.Infof("BPF program loaded on: %s File descriptor: %s", device.Name(), strconv.Itoa(fd))
				udsServer.AddDevice(device.Name(), fd)
			}

			if pm.BpfMapPinningEnable {
				logging.Infof("Loading BPF program on device: %s and pinning the map", device.Name())
				pinPath, err := pm.Pbm.Manager.CreateBPFFS()
				if err != nil {
					logging.Errorf("Error Creating the BPFFS: %v", err)
					return &response, err
				}

				err = pm.BpfHandler.LoadBpfPinXskMap(device.Name(), pinPath)
				if err != nil {
					logging.Errorf("Error loading BPF Program on interface %s and pinning the map: %v", device.Name(), err)
					return &response, err
				}

				pm.Pbm.Manager.AddMap(device.Name(), pinPath)

				//FULL PATH WILL INCLUDE THE XSKMAP...
				fullPath := pinPath + constants.Bpf.Xsk_map
				containerMapPath := constants.Bpf.BpfMapPodPath + device.Name() + constants.Bpf.Xsk_map
				logging.Debugf("mapping %s to %s", fullPath, containerMapPath)
				cresp.Mounts = append(cresp.Mounts, &pluginapi.Mount{
					HostPath:      fullPath,
					ContainerPath: containerMapPath,
					ReadOnly:      false,
				})
			}
		}

		envVar := constants.Devices.EnvVarList + strings.ToUpper(pm.Name)
		envs[envVar] = strings.Join(crqt.DevicesIDs, " ")
		envsPrint, err := tools.PrettyString(envs)
		if err != nil {
			logging.Errorf("Error printing container environment variables: %v", err)
		} else {
			logging.Debugf("Container environment variables: %s", envsPrint)
		}
		cresp.Envs = envs
		response.ContainerResponses = append(response.ContainerResponses, cresp)
	}

	if udsEnabled {
		udsServer.Start()
	}

	return &response, nil
}

/*
GetDevicePluginOptions is part of the device plugin API.
Unused.
*/
func (pm *PoolManager) GetDevicePluginOptions(context.Context, *pluginapi.Empty) (*pluginapi.DevicePluginOptions, error) {
	return &pluginapi.DevicePluginOptions{}, nil
}

/*
PreStartContainer is part of the device plugin API.
Unused.
*/
func (pm *PoolManager) PreStartContainer(context.Context, *pluginapi.PreStartContainerRequest) (*pluginapi.PreStartContainerResponse, error) {
	return &pluginapi.PreStartContainerResponse{}, nil
}

/*
GetPreferredAllocation is part of the device plugin API.
Unused.
*/
func (pm *PoolManager) GetPreferredAllocation(context.Context, *pluginapi.PreferredAllocationRequest) (*pluginapi.PreferredAllocationResponse, error) {
	return &pluginapi.PreferredAllocationResponse{}, nil
}

func (pm *PoolManager) registerWithKubelet() error {
	conn, err := grpc.NewClient("unix:"+pluginapi.KubeletSocket, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("error connecting to Kubelet: %w", err)
	}
	defer conn.Close()

	client := pluginapi.NewRegistrationClient(conn)

	reqt := &pluginapi.RegisterRequest{
		Version:      pluginapi.Version,
		Endpoint:     pm.DpAPIEndpoint,
		ResourceName: pm.DevicePrefix + "/" + pm.Name,
	}

	_, err = client.Register(context.Background(), reqt)
	if err != nil {
		return fmt.Errorf("error registering with Kubelet: %w", err)
	}

	return nil
}

func (pm *PoolManager) startGRPC() error {
	if err := pm.cleanup(); err != nil {
		return err
	}

	sock, err := net.Listen("unix", pm.DpAPISocket)
	if err != nil {
		return err
	}

	pm.DpAPIServer = grpc.NewServer([]grpc.ServerOption{}...)
	pluginapi.RegisterDevicePluginServer(pm.DpAPIServer, pm)
	go func() {
		if err := pm.DpAPIServer.Serve(sock); err != nil {
			logging.Errorf("API Server socket error: %v", err)
		}
	}()
	deadline := time.Now().Add(5 * time.Second)
	var dialErr error
	for time.Now().Before(deadline) {
		var c net.Conn
		c, dialErr = net.DialTimeout("unix", pm.DpAPISocket, time.Second)
		if dialErr == nil {
			c.Close()
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if dialErr != nil {
		logging.Errorf("Unable to establish test connection with %s gRPC server: %v", pm.Name, dialErr)
		return dialErr
	}
	logging.Debugf(pm.DevicePrefix+"/%s started serving on %s", pm.Name, pm.DpAPISocket)

	return nil
}

func (pm *PoolManager) stopGRPC() {
	if pm.DpAPIServer != nil {
		pm.DpAPIServer.Stop()
		pm.DpAPIServer = nil
	}
}

func (pm *PoolManager) cleanup() error {
	if err := os.Remove(pm.DpAPISocket); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
