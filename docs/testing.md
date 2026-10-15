# Testing the AF_XDP Device Plugin and CNI

This document describes the environments in which the plugins can be tested and what
should be tested in each. Three tiers are covered: unit tests (no cluster), functional
testing on [Kind](#kind-functional), and realistic testing on
[hardware or VMs](#bare-metal-or-vm-realistic). The design of the bpfman mode under test
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

## Bare metal or VM (realistic)

For data path fidelity use a node with an Intel NIC supported for zero-copy AF_XDP
(`i40e`, `ice`/E810 -- see `constants.Drivers.ZeroCopy`). A single node is enough;
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
