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
bpfmanTest is the bpfman mode counterpart of udsTest. It runs inside an
AF_XDP pod and verifies that the XSKMAP of each allocated netdev is exposed
by the bpfman CSI driver:

  - the map pin exists at <mapsMountPath>/<netdev>/xsks_map
  - it lives on a bpffs mount
  - the (unprivileged) pod can open it via BPF_OBJ_GET, which is what an
    AF_XDP application does to insert its sockets into the map

The netdevs are learned from the AFXDP_DEVICES_<pool> environment variables
set by the device plugin. The map mount path defaults to
/run/xdp/maps and can be overridden with the -maps-path flag.
*/
package main

import (
	"fmt"
	"os"
	"strings"
	"unsafe"

	"github.com/redhat-et/afxdp-plugins-for-kubernetes/constants"
	"golang.org/x/sys/unix"
)

const (
	exitOk  = 0
	exitErr = 1
)

// bpfObjGetAttr mirrors the kernel's union bpf_attr for the BPF_OBJ_GET
// command: a pointer to the pin path, followed by the (unused here) fd and
// flags fields.
type bpfObjGetAttr struct {
	pathName  uint64
	bpfFd     uint32
	fileFlags uint32
}

func main() {
	mapsPath := constants.Bpfman.CsiMapPodPath

	for _, arg := range os.Args[1:] {
		if strings.HasPrefix(arg, "-maps-path=") {
			mapsPath = strings.TrimPrefix(arg, "-maps-path=")
		}
	}

	devices := allocatedDevices()
	if len(devices) == 0 {
		fmt.Println("Test App Error: no AFXDP_DEVICES_<pool> environment variables found")
		os.Exit(exitErr)
	}

	fmt.Printf("Test App - Allocated devices: %v\n", devices)
	fmt.Printf("Test App - Looking for maps under: %s\n", mapsPath)

	failed := false
	for _, dev := range devices {
		if !checkMap(mapsPath, dev) {
			failed = true
		}
	}

	if failed {
		fmt.Println("Test App - FAILED")
		os.Exit(exitErr)
	}

	fmt.Println("Test App - PASSED")
	os.Exit(exitOk)
}

// allocatedDevices collects the netdevs from every AFXDP_DEVICES_<pool>
// environment variable set by the device plugin.
func allocatedDevices() []string {
	var devices []string

	for _, env := range os.Environ() {
		if !strings.HasPrefix(env, constants.Devices.EnvVarList) {
			continue
		}
		_, value, found := strings.Cut(env, "=")
		if !found {
			continue
		}
		for _, dev := range strings.Fields(value) {
			if dev != "" {
				devices = append(devices, dev)
			}
		}
	}

	return devices
}

// checkMap verifies one netdev's XSKMAP and returns true on success.
func checkMap(mapsPath, dev string) bool {
	mapPath := fmt.Sprintf("%s%s/%s", mapsPath, dev, constants.Bpfman.CsiMapName)

	fmt.Printf("Test App - Checking XSKMAP for device %s at %s\n", dev, mapPath)

	// The pin must exist.
	if _, err := os.Stat(mapPath); err != nil {
		fmt.Printf("Test App Error: XSKMAP pin not accessible: %v\n", err)
		return false
	}
	fmt.Printf("Test App - XSKMAP pin exists: %s\n", mapPath)

	// It must live on a bpffs mount.
	var st unix.Statfs_t
	if err := unix.Statfs(mapPath, &st); err != nil {
		fmt.Printf("Test App Error: statfs failed: %v\n", err)
		return false
	}
	if st.Type != unix.BPF_FS_MAGIC {
		fmt.Printf("Test App Error: %s is not on a bpffs mount (magic 0x%x)\n", mapPath, st.Type)
		return false
	}
	fmt.Println("Test App - XSKMAP pin is on a bpffs mount")

	// The pod must be able to open the map; this is what an AF_XDP
	// application does before inserting its sockets into the map.
	fd, err := bpfObjGet(mapPath)
	if err != nil {
		fmt.Printf("Test App Error: BPF_OBJ_GET failed: %v\n", err)
		return false
	}
	defer unix.Close(fd)
	fmt.Printf("Test App - XSKMAP opened by unprivileged pod, fd: %d\n", fd)

	return true
}

// bpfObjGet opens a pinned BPF object (BPF_OBJ_GET) and returns its fd.
func bpfObjGet(path string) (int, error) {
	cpath, err := unix.BytePtrFromString(path)
	if err != nil {
		return -1, err
	}

	attr := bpfObjGetAttr{pathName: uint64(uintptr(unsafe.Pointer(cpath)))}

	r0, _, errno := unix.Syscall(unix.SYS_BPF, unix.BPF_OBJ_GET, uintptr(unsafe.Pointer(&attr)), unsafe.Sizeof(attr))
	if errno != 0 {
		return -1, errno
	}

	return int(r0), nil
}
