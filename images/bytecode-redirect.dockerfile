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
# Build (requires the .o from `make buildxdp`):
#   make bytecode-image BYTECODE_IMAGE=quay.io/<org>/xdp-afxdp-redirect
# Push:
#   make bytecode-push BYTECODE_IMAGE=quay.io/<org>/xdp-afxdp-redirect
FROM scratch
COPY internal/bpf/xdp-afxdp-redirect/xdp_afxdp_redirect_bpfman.o /xdp_afxdp_redirect_bpfman.o
LABEL io.ebpf.programs="{\"xdp_afxdp_redirect\":\"xdp\"}"
LABEL io.ebpf.maps="{\"xsks_map\":\"xskmap\"}"
