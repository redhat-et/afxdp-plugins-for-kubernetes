# Copyright(c) Red Hat Inc.
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# OCI bytecode image for the AF_XDP device plugin's default XDP program,
# following bpfman's eBPF bytecode image specification: a single layer
# holding the object file plus io.ebpf.programs / io.ebpf.maps labels.
#
# bpfman pulls and caches the image itself, so - unlike BpfmanBytecodeFile -
# no shared filesystem is needed between the device plugin and bpfman.
# Point a pool's BpfmanBytecodeImage at the pushed reference.
#
# The object is built in the first stage (same flags as
# internal/bpf/xdp-afxdp-redirect/Makefile) so the image builds from a clean
# checkout. Override the XSKMAP size by changing XSK_MAP_MAX_ENTRIES in both
# places. Multi-arch (linux/amd64, linux/arm64): the object is arch-specific
# through its BTF; build with buildx --platform linux/amd64,linux/arm64.
#
# Build and push:
#   make bytecode-push BYTECODE_IMAGE=quay.io/<org>/xdp-afxdp-redirect
FROM ubuntu:24.04 AS builder
ARG TARGETARCH
RUN apt-get update && \
    apt-get install -y --no-install-recommends clang libbpf-dev linux-libc-dev && \
    rm -rf /var/lib/apt/lists/*
COPY internal/bpf/xdp-afxdp-redirect/xdp_afxdp_redirect.c /src/
# Linux UAPI headers live in the multiarch include directory; -target bpf
# drops it from clang's default search path.
RUN case "${TARGETARCH}" in \
        amd64) ARCH_INCLUDE=/usr/include/x86_64-linux-gnu ;; \
        arm64) ARCH_INCLUDE=/usr/include/aarch64-linux-gnu ;; \
        *) echo "unsupported TARGETARCH: ${TARGETARCH}" >&2; exit 1 ;; \
    esac && \
    clang -target bpf \
        -D __BPF_TRACING__ \
        -I/usr/include/bpf \
        -I"${ARCH_INCLUDE}" \
        -Wall -Wno-unused-value -Wno-pointer-sign -Wno-compare-distinct-pointer-types \
        -O2 -g -DBPFMAN -DXSK_MAP_MAX_ENTRIES=64 \
        -c /src/xdp_afxdp_redirect.c -o /xdp_afxdp_redirect_bpfman.o

FROM scratch
COPY --from=builder /xdp_afxdp_redirect_bpfman.o /xdp_afxdp_redirect_bpfman.o
LABEL io.ebpf.programs="{\"xdp_afxdp_redirect\":\"xdp\"}"
LABEL io.ebpf.maps="{\"xsks_map\":\"xskmap\"}"
