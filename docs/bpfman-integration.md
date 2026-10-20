# bpfman Integration Design

This document describes how the AF_XDP device plugin and CNI integrate with
[bpfman](https://github.com/bpfman/bpfman), the design decisions behind the
integration, and how it relates to the legacy modes.

## Overview

The plugins support three ways of managing the eBPF side of AF_XDP networking
(the XDP program that redirects packets into AF_XDP sockets, and the XSKMAP it
uses). All three remain fully supported; bpfman mode is the recommended one for
new deployments.

| | UDS mode (legacy, default) | Map pinning (legacy) | bpfman mode (recommended) |
| --- | --- | --- | --- |
| XDP program loaded by | device plugin (libxdp) | xdp-loader (subprocess) | bpfman (gRPC) |
| Program lifecycle | pod (loaded at `Allocate`, unloaded by the CNI at pod delete) | pod (same) | pool (loaded at pool start, unloaded at pool termination) |
| XSKMAP shared with pod via | UDS + SCM_RIGHTS fd passing | per-allocation bpffs + hostPath mount | bpfman CSI driver (per-pod bpffs) |
| Pod privileges | none for the map; CAP_BPF on kernels < 5.19 to open it | CAP_BPF on kernels < 5.19 | none on kernels >= 5.19 |
| Device plugin securityContext | SYS_ADMIN, NET_ADMIN | privileged (mount propagation) | SYS_ADMIN, NET_ADMIN |
| Custom XDP programs | no | no (fixed object) | yes, per pool (local file or OCI bytecode image) |

## bpfman mode

### Components and flow

```
                 ┌────────────┐  register   ┌─────────┐
                 │  kubelet   │◄────────────┤  AF_XDP │
                 └─────┬──────┘  resource   │ device  │
                       │                   │ plugin  │      Load/Attach/Unload
        Allocate() ───►│                   └────┬────┘      (gRPC, unix socket)
                       │                        │           /run/bpfman-sock/bpfman.sock
        CSI volume ───►│                        ▼
        NodePublishVol │                   ┌────────┐
                       │                   │ bpfman │── dispatcher ──► XDP prog
                       │                   │  + CSI │    attach          │
                       ▼                   └────┬───┘                    ▼
                 ┌────────────┐   re-pin        │                  ┌──────────┐
                 │ AF_XDP pod │◄────────────────┘                  │ XSKMAP   │
                 │ (unpriv.) │   /run/xdp/maps/<netdev>/xsks_map   │ (pinned) │
                 └────────────┘                                      └──────────┘
```

1. **Pool start** (`poolManager.Init`): for every netdev in the pool the device
   plugin loads the XDP redirect program via bpfman and attaches it to the
   netdev (XDP dispatcher slot, configurable priority, proceed-on
   `pass, dispatcher_return`). The program carries:
   - `bpfman.io/ProgramName: afxdp-<pool>-<netdev>` - the name pods can select
   - `afxdp.io/owner: afxdp-device-plugin`, `afxdp.io/pool`, `afxdp.io/device` -
     used for reconciliation and by the CSI selector
   In CDQ pools, subfunctions are activated (devlink) *before* their programs
   are loaded. Pools are only registered with kubelet after all programs are
   loaded, so no pod can be scheduled onto a device without its XSKMAP.

2. **Pod creation**: kubelet calls `Allocate()`. The device plugin only sets the
   `AFXDP_DEVICES_<pool>` environment variable (and activates CDQ subfunctions
   if needed); no BPF work happens here. The pod spec carries a bpfman CSI
   volume that exposes the pool's XSKMAPs (see below), and multus/the CNI move
   the allocated netdev into the pod as usual.

3. **Pod teardown**: the CNI (with `"bpfman": true` in the NAD) returns the
   netdev to the host network namespace but does not unload the XDP program and
   does not delete CDQ subfunctions - those follow the pool lifecycle.

4. **Pool termination**: the device plugin unloads all of the pool's programs
   via bpfman and deletes eagerly-created CDQ subfunctions.

5. **Device plugin restart**: the plugin lists bpfman programs carrying its
   owner/pool metadata, adopts programs whose devices are still in the pool and
   whose bytecode source, function name, and priority match the current pool
   configuration (`afxdp.io/bytecode-*`, `afxdp.io/function`, `afxdp.io/priority`
   metadata), unloads mismatched programs for reload, and unloads any orphans.
   If loading fails partway through pool start, programs loaded in that attempt
   are rolled back and any eagerly-created CDQ subfunctions are deleted before
   the pool registers with kubelet.

### Why the program follows the pool lifecycle

Kubelet mounts a pod's volumes - including the CSI `NodePublishVolume` call that
re-pins the XSKMAP into the pod - *before* it calls the device plugin's
`Allocate()` (in Kubernetes >= 1.x, `syncPod` waits on
`volumeManager.WaitForAttachAndMount` before `containerRuntime.SyncPod`, which
is where device allocation runs). A program loaded lazily at `Allocate` could
never satisfy the CSI mount: the mount would wait for a program that is only
loaded after the mount succeeds.

Loading at pool start instead makes the pinned XSKMAP exist before any pod is
scheduled. Idle pooled devices are unaffected: the redirect program falls back
to `XDP_PASS` when no AF_XDP socket is inserted into the map for the packet's
queue, so traffic simply continues to the kernel network stack. These devices
are dedicated to AF_XDP pools by configuration, so an always-attached XDP
program is the natural lifecycle.

### How pods get the right XSKMAP

Multus injects the `deviceID` (the netdev allocated to the pod by kubelet) into
the CNI configuration at runtime, so netdev names - especially CDQ subfunction
names - are generally not known when a pod spec is written. A static
`csi.bpfman.io/program: afxdp-<pool>-<netdev>` volume attribute therefore only
works for pools with a single, well-known device.

For the general case the pod uses the bpfman CSI driver's metadata selector
(requires the `csi.bpfman.io/programs-by-metadata` support):

```yaml
volumes:
- name: afxdp-maps
  csi:
    driver: csi.bpfman.io
    volumeAttributes:
      csi.bpfman.io/programs-by-metadata: afxdp.io/pool=myPool
      csi.bpfman.io/subpath-metadata: afxdp.io/device
      csi.bpfman.io/maps: xsks_map
```

The CSI driver re-pins the requested maps of *every* program matching the
selector into a per-pod bpffs, each under the subdirectory named by the
program's `afxdp.io/device` metadata. The pod sees
`/run/xdp/maps/<netdev>/xsks_map` for each netdev in the pool and picks its own
using the `AFXDP_DEVICES_<pool>` environment variable. The per-pod bpffs is
group-owned by the pod's fsGroup, so no privileges are needed to open the map
(kernel >= 5.19).

Note: every pod in a pool can see (and open) the whole pool's maps, but a pod
can only insert AF_XDP sockets into the map of a netdev that resides in its own
network namespace, so cross-pod use of another device's map is not possible;
deleting another pod's entries is (as with any shared resource within a pool).

### The XDP program

The program is the same redirect program as in legacy modes
(`internal/bpf/xdp-afxdp-redirect/xdp_afxdp_redirect.c`), compiled twice:

- `xdp_afxdp_redirect.o` - pins the XSKMAP by name (`LIBBPF_PIN_BY_NAME`),
  used with `xdp-loader` in legacy pinning mode.
- `xdp_afxdp_redirect_bpfman.o` - no pinning attribute, used with bpfman.
  bpfman pins `LIBBPF_PIN_BY_NAME` maps into a *shared* directory where
  programs would wrongly reuse a single XSKMAP across netdevs; without the
  attribute each program gets its own map, pinned under its program map
  directory and re-pinned per pod by the CSI driver.

Both builds are single-step, BTF-aware clang compilations. BTF is required for
dispatcher (freplace) based attachment used by libxdp and bpfman. The XSKMAP
size defaults to 64 queue entries and can be raised at build time
(`make XSK_MAP_MAX_ENTRIES=<n>`) for NICs with more queues. The map must keep
its name (`xsks_map`); DPDK's AF_XDP PMD relies on it when loading custom
programs.

## Configuration reference

Device plugin (`config.json`):

| Field | Level | Default | Meaning |
| --- | --- | --- | --- |
| `bpfmanSocket` | global | `/run/bpfman-sock/bpfman.sock` | bpfman gRPC unix socket |
| `BpfmanEnable` | pool | `false` | use bpfman for this pool's XDP programs |
| `BpfmanBytecodeFile` | pool | `/afxdp/xdp_afxdp_redirect_bpfman.o` | local XDP object to load |
| `BpfmanBytecodeImage` | pool | - | OCI bytecode image to load (exclusive with the file) |
| `BpfmanFunction` | pool | `xdp_afxdp_redirect` | function name within the object |
| `BpfmanPriority` | pool | `10` | XDP priority (lower runs earlier) |

Validation rejects `BpfmanEnable` combined with `BpfMapPinningEnable`, setting
a bytecode source while bpfman is disabled, and both bytecode sources at once.

CNI (NAD `spec.config`): `"bpfman": true` skips the XDP program unload and CDQ
subfunction deletion at pod teardown. `skipUnloadBpf` remains for manually
managing the program.

## Deployment requirements

- bpfman daemon with the gRPC API and CSI support on every node (e.g. deployed
  by the bpfman operator; CSI requires Kubernetes >= 1.26)
- Kernel >= 5.6 for dispatcher attachment; >= 5.19 for unprivileged map access
- The device plugin mounts `/run/bpfman-sock` (see
  `deployments/daemonset-bpfman.yaml`); no bpffs/UDS hostPath mounts are needed
  and the daemonset is not privileged
- When running bpfman's CSI driver outside the operator's manifest, its
  DaemonSet must mount `/var/lib/kubelet/pods` with
  `mountPropagation: Bidirectional` - the CSI driver bind-mounts per-pod bpffs
  volumes into that path, and without the shared propagation those mounts stay
  private to the bpfman container's mount namespace and pods receive empty
  directories. (The operator's DaemonSet already does this.)
- The device plugin re-registers its pools with kubelet automatically after a
  kubelet restart (kubelet deletes the device-plugin sockets at startup);
  `afxdp/<pool>` capacity recovers within a few seconds.

## Application integration

- **Go/C applications**: open `/run/xdp/maps/<netdev>/xsks_map` (bpffs pin) and
  insert AF_XDP socket fds with `bpf_map_update_elem`, using the netdevs from
  `AFXDP_DEVICES_<pool>`. See `test/e2e/bpfman/bpfmanTest.go` for an example.
- **DPDK**: AF_XDP PMD with `use_pinned_map=1,dp_path=/run/xdp/maps/<netdev>/xsks_map`
  (bpfman mode), `use_cni=1` (legacy UDS mode) or
  `use_pinned_map=1,dp_path=/tmp/afxdp_dp/<netdev>/xsks_map` (legacy pinning
  mode).

## Testing

See [testing.md](./testing.md) for the environments and the test matrix, and
[../test/e2e/README.md](../test/e2e/README.md) for the bpfman mode e2e test.

## Limitations and future work

- The pool's CSI volume exposes all of the pool's maps to each pod in the pool
  (see the security note above). A stricter per-allocation selection would
  require passing pod identity into the CSI lookup.
- XDP programs attached by other loaders on a pooled netdev conflict with
  bpfman's dispatcher (one XDP program per interface); attach competing programs
  through bpfman instead.
- CDQ subfunctions live for the pool lifetime in bpfman mode (rather than the
  pod lifetime), trading devlink resources for race-free startup.
