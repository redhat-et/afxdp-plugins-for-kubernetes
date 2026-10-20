/*
 * Copyright(c) Red Hat Inc.
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

package bpfman

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConfigMatchesMetadata(t *testing.T) {
	fileOpts := XDPLoadOptions{
		BytecodeFile: "/afxdp/xdp_afxdp_redirect_bpfman.o",
		FunctionName: "xdp_afxdp_redirect",
		Priority:     10,
	}
	assert.True(t, ConfigMatchesMetadata(fileOpts, ConfigMetadata(fileOpts)))
	assert.False(t, ConfigMatchesMetadata(fileOpts, map[string]string{
		MetadataKeyBytecodeFile: "/other.o",
		MetadataKeyFunction:     "xdp_afxdp_redirect",
		MetadataKeyPriority:     "10",
	}))

	imageOpts := XDPLoadOptions{
		BytecodeImage: "quay.io/afxdp/xdp-redirect:latest",
		FunctionName:  "xdp_afxdp_redirect",
		Priority:      50,
	}
	assert.True(t, ConfigMatchesMetadata(imageOpts, ConfigMetadata(imageOpts)))
	assert.False(t, ConfigMatchesMetadata(imageOpts, map[string]string{
		MetadataKeyBytecodeImage: "quay.io/afxdp/xdp-redirect:latest",
		MetadataKeyBytecodeFile:  "/afxdp/xdp_afxdp_redirect_bpfman.o",
		MetadataKeyFunction:      "xdp_afxdp_redirect",
		MetadataKeyPriority:      "50",
	}))
}
