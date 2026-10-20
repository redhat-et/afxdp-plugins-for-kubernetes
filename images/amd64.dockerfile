# Copyright(c) 2022 Intel Corporation.
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

FROM docker.io/library/golang:1.25@sha256:2c7ebcaf2c1032b4b4d15df14b7edf3221b1e45dadfaa36ab6a2f8555feacaa6 AS cnibuilder
COPY . /usr/src/afxdp_k8s_plugins
WORKDIR /usr/src/afxdp_k8s_plugins
# libdw-dev and libzstd-dev are required for static linking against Debian trixie's
# newer elfutils (0.191+) where libelf.a references eu_search_tree_* and ZSTD symbols.
RUN apt-get update \
&& apt-get -y install --no-install-recommends libxdp-dev libdw-dev libzstd-dev \
&& apt-get -y install -o APT::Keep-Downloaded-Packages=false --no-install-recommends clang \
&& apt-get -y install -o APT::Keep-Downloaded-Packages=false --no-install-recommends llvm \
&& apt-get -y install -o APT::Keep-Downloaded-Packages=false --no-install-recommends gcc-multilib \
&& sed -i 's|LDFLAGS: -L. -lxdp -lbpf -lelf -lz|LDFLAGS: -L. -lxdp -lbpf -lelf -lz -lzstd -ldw|' internal/bpf/bpfWrapper.go \
&& make buildcni

FROM docker.io/library/golang:1.25-alpine@sha256:56961d79ea8129efddcc0b8643fd8a5416b4e6228cfd477e3fd61deb2672c587 AS dpbuilder
COPY . /usr/src/afxdp_k8s_plugins
WORKDIR /usr/src/afxdp_k8s_plugins
RUN apk add --no-cache build-base \
&& apk add --no-cache libbsd-dev \
&& apk add --no-cache libxdp-dev \
&& apk add --no-cache libbpf-dev \
&& apk add --no-cache llvm \
&& apk add --no-cache clang \
&& make builddp

FROM docker.io/library/alpine:3.21@sha256:48b0309ca019d89d40f670aa1bc06e426dc0931948452e8491e3d65087abc07d
RUN apk --no-cache -U add iproute2-rdma acl \
      && apk add --no-cache xdp-tools
COPY --from=cnibuilder /usr/src/afxdp_k8s_plugins/bin/afxdp /afxdp/afxdp
COPY --from=dpbuilder /usr/src/afxdp_k8s_plugins/bin/afxdp-dp /afxdp/afxdp-dp
COPY --from=dpbuilder /usr/src/afxdp_k8s_plugins/images/entrypoint.sh /afxdp/entrypoint.sh
COPY --from=dpbuilder /usr/src/afxdp_k8s_plugins/internal/bpf/xdp-pass/xdp_pass.o /afxdp/xdp_pass.o
COPY --from=dpbuilder /usr/src/afxdp_k8s_plugins/internal/bpf/xdp-afxdp-redirect/xdp_afxdp_redirect.o /afxdp/xdp_afxdp_redirect.o
COPY --from=dpbuilder /usr/src/afxdp_k8s_plugins/internal/bpf/xdp-afxdp-redirect/xdp_afxdp_redirect_bpfman.o /afxdp/xdp_afxdp_redirect_bpfman.o
# Root is required: entrypoint manages BPF map pinning and kernel networking setup.
USER 0
ENTRYPOINT ["/afxdp/entrypoint.sh"]
