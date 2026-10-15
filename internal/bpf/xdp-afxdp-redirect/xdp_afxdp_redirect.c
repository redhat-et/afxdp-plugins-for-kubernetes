/*
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
// clang-format off
#include <linux/types.h>
#include <bpf/bpf_helpers.h>
#include <linux/bpf.h>
// clang-format on

/* Number of netdev queues the XSKMAP can hold entries for. Each entry maps
 * a queue id to the AF_XDP socket bound to that queue. Override at build
 * time for NICs with more than the default number of queues:
 *   make XSK_MAP_MAX_ENTRIES=256
 */
#ifndef XSK_MAP_MAX_ENTRIES
#define XSK_MAP_MAX_ENTRIES 64
#endif

#ifndef BPFMAN
/* When loaded via xdp-loader (legacy pinning mode) the map is pinned by
 * name into the pin path given at load time.
 *
 * The bpfman build (-DBPFMAN) omits pinning: bpfman pins LIBBPF_PIN_BY_NAME
 * maps into a shared directory where programs would wrongly reuse a single
 * XSKMAP across netdevs. Without the attribute each loaded program gets its
 * own map, pinned by bpfman under the program's map directory and exposed
 * to pods by the bpfman CSI driver.
 */
#define XSK_MAP_PINNING LIBBPF_PIN_BY_NAME
#else
#define XSK_MAP_PINNING 0
#endif

struct {
	__uint(type, BPF_MAP_TYPE_XSKMAP);
	__type(key, __u32);
	__type(value, __u32);
	__uint(max_entries, XSK_MAP_MAX_ENTRIES);
	__uint(pinning, XSK_MAP_PINNING);
} xsks_map SEC(".maps");

SEC("xdp")
int xdp_afxdp_redirect(struct xdp_md *ctx) {
	int index = ctx->rx_queue_index;

	/* A set entry here means that the correspnding queue_id
	 * has an active AF_XDP socket bound to it. */
	if (bpf_map_lookup_elem(&xsks_map, &index))
		return bpf_redirect_map(&xsks_map, index, 0);

	return XDP_PASS;
}

char _license[] SEC("license") = "Dual BSD";
