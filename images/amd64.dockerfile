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

# Bookworm rather than trixie: trixie's elfutils (0.192) ships a libelf.a that
# references eu_search_tree_* symbols provided by no trixie package, which
# breaks static linking of the CNI.
FROM public.ecr.aws/docker/library/golang:1.25-bookworm@sha256:3b4a11519ad929d1e1d261a12cff056f0c85b735253d7d861346b9c6f8b36437 AS cnibuilder
COPY . /usr/src/afxdp_k8s_plugins
WORKDIR /usr/src/afxdp_k8s_plugins
RUN apt-get update \
&& apt-get -y install --no-install-recommends libxdp-dev zlib1g-dev \
&& make buildcni

FROM public.ecr.aws/docker/library/golang:1.25-alpine@sha256:1ae0735f00daffa3aaf1363a5184c0d2dc55c78e3db4ec70241cdac97bf84b59 AS dpbuilder
COPY . /usr/src/afxdp_k8s_plugins
WORKDIR /usr/src/afxdp_k8s_plugins
RUN apk add --no-cache build-base \
&& apk add --no-cache libbsd-dev \
&& apk add --no-cache libxdp-dev \
&& apk add --no-cache libbpf-dev \
&& apk add --no-cache llvm \
&& apk add --no-cache clang \
&& make builddp

FROM public.ecr.aws/docker/library/alpine:3.21@sha256:ce64758a109eb420d874a118f87920e625e12d3634e03b4a5573fd9f6e5d3507
RUN apk --no-cache -U add iproute2-rdma acl \
      && apk add --no-cache xdp-tools
COPY --from=cnibuilder /usr/src/afxdp_k8s_plugins/bin/afxdp /afxdp/afxdp
COPY --from=dpbuilder /usr/src/afxdp_k8s_plugins/bin/afxdp-dp /afxdp/afxdp-dp
COPY --from=dpbuilder /usr/src/afxdp_k8s_plugins/images/entrypoint.sh /afxdp/entrypoint.sh
COPY --from=dpbuilder /usr/src/afxdp_k8s_plugins/internal/bpf/xdp-pass/xdp_pass.o /afxdp/xdp_pass.o
COPY --from=dpbuilder /usr/src/afxdp_k8s_plugins/internal/bpf/xdp-afxdp-redirect/xdp_afxdp_redirect.o /afxdp/xdp_afxdp_redirect.o
ENTRYPOINT ["/afxdp/entrypoint.sh"]
