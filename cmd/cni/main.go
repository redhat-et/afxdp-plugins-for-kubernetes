/*
 * Copyright(c) 2022 Intel Corporation.
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

package main

import (
	"github.com/containernetworking/cni/pkg/skel"
	cniversion "github.com/containernetworking/cni/pkg/version"
	"github.com/redhat-et/afxdp-plugins-for-kubernetes/internal/cni"
)

func main() {
	skel.PluginMain(
		func(args *skel.CmdArgs) error {
			err := cni.CmdAdd(args)
			if err != nil {
				return err
			}
			return nil
		},
		func(args *skel.CmdArgs) error {
			return cni.CmdCheck(args)
		},
		func(args *skel.CmdArgs) error { return cni.CmdDel(args) },
		cniversion.All, "AF_XDP CNI Plugin")
}
