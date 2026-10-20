# Testing the AF_XDP Device Plugin and CNI

This document describes the environments in which the plugins can be tested and what
should be tested in each. Four tiers are covered: unit tests (no cluster), functional
testing on [Kind](#kind-functional), [cloud instances and VMs](#cloud-instances-and-vms-virtual-nics)
(virtual NICs, copy or zero-copy depending on the driver), and realistic testing on
[hardware](#bare-metal-or-vm-realistic). The design of the bpfman mode under test
is described in [bpfman-integration.md](./bpfman-integration.md).

## Unit tests (development machine or CI, Linux)

The Go tests cover the device plugin config validation, the `Allocate()` flows (UDS,
pinning and bpfman modes), the UDS server protocol, the CNI config, and the bpfman
gRPC client (against an in-process fake bpfman gRPC server). Building requires cgo
headers for libxdp/libbpf:

```sh
sudo apt install libxdp-dev libbpf-dev gcc
make test          # or: go test ./...
```

The XDP programs are compiled from `internal/bpf/xdp-*` with a single-step BTF-aware
clang build (BTF is required for dispatcher/freplace based attachment used by both
xdp-loader and bpfman). Verify they build with `make buildxdp`.

## Kind (functional)

Kind exercises the full Kubernetes control flow -- device plugin registration, multus,
CNI add/del, UDS handshake, map pinning -- using veth pairs. AF_XDP runs in copy mode
on veths, so there is no zero-copy performance fidelity, but all control plane behavior
is testable.

```sh
make run-on-kind          # creates the cluster, deploys the DP + CNI + multus
kubectl create -f examples/network-attachment-definition.yaml
kubectl create -f examples/kind-pod-spec.yaml
```

What to test on kind:

- Device pool discovery and `afxdp/<pool>` resource advertisement
- Pod allocation: `AFXDP_DEVICES_<pool>` env var, interface moved into the pod netns
- UDS mode: handshake (`/connect`, `/xsk_map_fd`, `/version`, `/fin`), fd passing,
  idle timeout behavior (`test/e2e/udsTest.go` with `-timeout-before/after-connect`)
- Map pinning mode: pinned `xsks_map` visible in the pod, BPFFS cleanup on pod delete
- Pod churn/soak: `test/e2e/e2e-test.sh --soak`
- Multi-pod / multi-container / multi-device permutations (`e2e-test.sh --full`)
- bpfman mode: deploy bpfman with CSI support in the kind cluster, then
  `deployments/daemonset-bpfman.yaml` + the bpfman examples; verify the XSKMAPs
  appear at `/run/xdp/maps/<netdev>/xsks_map` in the pod and that DP restarts
  re-adopt (not duplicate) the bpfman programs

## Cloud instances and VMs (virtual NICs)

Not everyone has access to physical AF_XDP-capable NICs. The support matrix below
was verified against the kernel sources; use `xdp-loader features <iface>` on the
instance to confirm (look for `XSK_ZEROCOPY` in the features list).

| Platform | Driver | AF_XDP mode | Since | Notes |
| --- | --- | --- | --- | --- |
| kind / containers | `veth` | copy | - | full control plane, no data path fidelity |
| AWS EC2 (Elastic Network Adapter) | `ena` | copy | - | ena has native XDP but no XSK support (verified through mainline v7.x); functional testing only |
| Local VM (QEMU/libvirt/KVM) | `virtio_net` | **zero-copy** (kernel >= 6.11), copy on older | v6.11 | best no-hardware zero-copy option; needs a >= 6.11 guest kernel |
| GCP (gVNIC) | `gve` | **zero-copy** (GQI-QPL format, kernel >= 6.4), copy on older | v6.4 | |
| Azure (accelerated networking) | `mlx5_core` | **zero-copy** | long-standing | mlx5 also supports devlink subfunctions, but the plugin's CDQ mode currently gates on the `ice` driver |
| Bare metal Intel | `i40e`, `ice`, `E810` | **zero-copy** | long-standing | full CDQ support on `ice` |

What virtual NICs cover:

- All control plane behavior: pool discovery, allocation, UDS handshake, bpfman
  load/attach/reconcile, CSI map exposure, unprivileged pod access, CNI add/del,
  pod churn, DPDK `use_cni`/`use_pinned_map` flows
- Data path in copy mode (including DPDK testpmd without `xdp_zerocopy=1`)

What they do not cover (needs zero-copy hardware): zero-copy performance,
`NETDEV_XDP_ACT_XSK_ZEROCOPY` verification, CDQ subfunction scaling (`ice` only),
hardware ethtool filters.

### AWS example

Any ENA instance works for functional testing (e.g. `c5n.2xlarge`/`m5n.2xlarge`
with 4+ queues). Use a distribution with kernel >= 5.19 so pods can open the
exposed maps without `CAP_BPF` (Ubuntu 24.04's 6.8 is a good fit; Amazon Linux
2023's 5.10 works for the plugins themselves but pods need `CAP_BPF` for map
access). The instance's primary interface is typically `ens5` (or `eth0` for the
first ENI) - both can be pooled after excluding them from the cluster's default
network; the prohibited-prefix list in `constants.Devices.Prohibited` does not
block `ens5`.

ENA-specific constraints found during validation:

- **XDP requires MTU <= ~3500**: the AWS intra-VPC default is jumbo 9001, which
  the ena driver rejects while XDP is attached - lower the pooled interface
  (e.g. `ip link set dev ens6 mtu 1500`). DHCP renewals re-apply the VPC MTU,
  so re-assert it before each test run.
- **Combined channels must be at most half the maximum**: ena needs dedicated
  XDP TX queues (`ethtool -L <if> combined <max/2>`), otherwise program
  attachment fails with "no enough space for allocating XDP queues".
- **Same-instance ENI hairpin is filtered** by the VPC router: traffic sent
  from the instance to its own secondary ENI IP does not loop back. Datapath
  tests need an external source, or an Elastic IP associated with the pooled
  ENI.
- **Runtime hugepage allocation** (`sysctl vm.nr_hugepages`) is only reflected
  in the node's allocatable capacity after a kubelet restart.

```sh
# on the instance: confirm the NIC and its XDP features
ethtool -i ens5 | grep driver        # -> ena
xdp-loader features ens5             # native XDP; no XSK_ZEROCOPY -> copy mode
```

Pool config for the device plugin:

```json
{
  "pools": [
    {
      "name": "myPool",
      "mode": "primary",
      "BpfmanEnable": true,
      "drivers": [ { "name": "ena" } ]
    }
  ]
}
```

### Local VM example (zero-copy without hardware)

A libvirt/QEMU VM with virtio NICs on a >= 6.11 kernel supports zero-copy
AF_XDP:

```sh
ethtool -i ens3 | grep driver        # -> virtio_net
xdp-loader features ens3             # XSK_ZEROCOPY present on >= 6.11
```

## Bare metal or VM (realistic)

For data path fidelity use a node with a NIC supporting zero-copy AF_XDP
(`i40e`, `ice`/E810, or the virtual options in the table above - `virtio_net` >= 6.11,
`gve` >= 6.4, `mlx5_core`). A single node is enough;
install Kubernetes + multus + a default CNI, then `make deploy`.

Recommended setup:

- Kernel >= 5.19 (6.x preferred): dispatcher attachment, unprivileged map access
  (`kernel.unprivileged_bpf_disabled` can stay at its default), busy polling
- bpfman (daemon or operator) with CSI support for bpfman mode testing
- `xdp-tools` (xdp-bench, xdp-trafficgen) for datapath load/verification
- DPDK with the AF_XDP PMD (testpmd) for application-level validation

What to test on hardware:

| Area | Test |
| --- | --- |
| Zero-copy | `xdp-loader features <ifname>` shows `XSK_ZEROCOPY`; xdp-bench `xsk-drop`/`xsk-tx` in native mode with copy-mode comparison |
| Queue scaling | XSKMAP covers all NIC queues (rebuild the program with `make XSK_MAP_MAX_ENTRIES=<n>` if > 64); multi-queue pods (DPDK `queue_count`) |
| UDS mode (legacy) | DPDK testpmd with `use_cni=1` -- full handshake, RX/TX, pod delete cleans the XDP program |
| Map pinning mode (legacy) | testpmd with `use_pinned_map=1,dp_path=/tmp/afxdp_dp/<dev>/xsks_map` |
| bpfman mode (recommended) | testpmd with `use_pinned_map=1,dp_path=/run/xdp/maps/<dev>/xsks_map`; unprivileged pod (no CAP_BPF) on kernel >= 5.19; DP/pod restart behavior; XDP coexistence with other bpfman programs (proceed-on chain) |
| Busy polling | UDS mode `/config_busy_poll` via goclient/cclient; xdp-bench `--busy-poll` with `napi_defer_hard_irqs`/`gro_flush_timeout` sysctls |
| CDQ | `ice` driver + devlink subfunctions: pool with `mode: cdq`, SF creation, XDP on SFs, SF cleanup at pool termination (bpfman mode) or pod teardown (legacy mode) |
| Hardware filters | ethtool ntuple/`-X` commands from the NAD applied at CNI add, removed at del |
| Soak/churn | `e2e-test.sh --soak` (or the bpfman-mode equivalent) watching for leaks: bpfman programs, bpffs mounts, devlink ports, UDS sockets |

### DPDK application guidance

- With the UDS flow (legacy), run testpmd with `--vdev net_af_xdp1,iface=<dev>,use_cni=1`.
- With pinned maps, use `use_pinned_map=1` and point `dp_path` at the pin visible in
  the pod: `/tmp/afxdp_dp/<dev>/xsks_map` (pinning mode) or
  `/run/xdp/maps/<dev>/xsks_map` (bpfman mode via the bpfman CSI driver).
- DPDK inserts and removes its XSKs in the XSKMAP itself; the plugin only provides
  the map.
- `busy_budget` currently cannot be combined with `use_cni` in DPDK; use
  `use_pinned_map` with busy polling, or the UDS `/config_busy_poll` request from the
  application.

## CI

`.github/workflows/public-ci.yml` builds the plugins (libbpf from source + xdp-tools)
and runs the static checks. The e2e script and the kind flow are exercised manually or
from a self-hosted runner with suitable hardware.
