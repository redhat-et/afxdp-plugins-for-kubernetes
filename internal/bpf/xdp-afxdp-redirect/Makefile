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

CLANG ?= clang

# Number of queue entries in the XSKMAP, override for NICs with more queues.
XSK_MAP_MAX_ENTRIES ?= 64

# Linux UAPI headers (asm/types.h & friends) live in the multiarch include
# directory; -target bpf drops it from clang's default search path.
ARCH_INCLUDES := $(wildcard /usr/include/x86_64-linux-gnu /usr/include/aarch64-linux-gnu /usr/include/s390x-linux-gnu /usr/include/powerpc64le-linux-gnu)

# Single-step BTF-aware build. BTF (-g) is required for libxdp/bpfman
# dispatcher (freplace) based attachment and for CO-RE relocations.
CLANG_FLAGS = \
	-target bpf \
	-D __BPF_TRACING__ \
	-I/usr/include/bpf \
	$(addprefix -I,$(ARCH_INCLUDES)) \
	-Wall \
	-Wno-unused-value \
	-Wno-pointer-sign \
	-Wno-compare-distinct-pointer-types \
	-Werror \
	-O2 -g

all: xdp_afxdp_redirect.o xdp_afxdp_redirect_bpfman.o

# Legacy build used with xdp-loader in BPF map pinning mode: the XSKMAP is
# pinned by name (LIBBPF_PIN_BY_NAME) into the pin path passed to xdp-loader.
xdp_afxdp_redirect.o: xdp_afxdp_redirect.c
	$(CLANG) $(CLANG_FLAGS) -DXSK_MAP_MAX_ENTRIES=$(XSK_MAP_MAX_ENTRIES) -c -o $@ $<

# bpfman build used when the device plugin loads the program via bpfman: the
# XSKMAP is not pinned by name so each program gets its own map, pinned by
# bpfman under the program's map directory.
xdp_afxdp_redirect_bpfman.o: xdp_afxdp_redirect.c
	$(CLANG) $(CLANG_FLAGS) -DBPFMAN -DXSK_MAP_MAX_ENTRIES=$(XSK_MAP_MAX_ENTRIES) -c -o $@ $<

clean:
	rm -f *.o
