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

import "strconv"

const (
	// MetadataKeyBytecodeFile is the local bytecode object path used for a program.
	MetadataKeyBytecodeFile = "afxdp.io/bytecode-file"
	// MetadataKeyBytecodeImage is the OCI bytecode image URL used for a program.
	MetadataKeyBytecodeImage = "afxdp.io/bytecode-image"
	// MetadataKeyFunction is the XDP function name within the bytecode object.
	MetadataKeyFunction = "afxdp.io/function"
	// MetadataKeyPriority is the XDP dispatcher priority as a decimal string.
	MetadataKeyPriority = "afxdp.io/priority"
)

/*
ConfigMetadata returns metadata entries that describe how a program was loaded.
They are stored on bpfman programs and used to detect configuration drift when
adopting programs across device plugin restarts.
*/
func ConfigMetadata(opts XDPLoadOptions) map[string]string {
	metadata := map[string]string{
		MetadataKeyFunction: opts.FunctionName,
		MetadataKeyPriority: strconv.FormatInt(int64(opts.Priority), 10),
	}
	if opts.BytecodeFile != "" {
		metadata[MetadataKeyBytecodeFile] = opts.BytecodeFile
	}
	if opts.BytecodeImage != "" {
		metadata[MetadataKeyBytecodeImage] = opts.BytecodeImage
	}
	return metadata
}

/*
ConfigMatchesMetadata reports whether bpfman program metadata matches the given
load options (bytecode source, function name, and priority).
*/
func ConfigMatchesMetadata(opts XDPLoadOptions, metadata map[string]string) bool {
	for k, v := range ConfigMetadata(opts) {
		if metadata[k] != v {
			return false
		}
	}
	if opts.BytecodeFile != "" && metadata[MetadataKeyBytecodeImage] != "" {
		return false
	}
	if opts.BytecodeImage != "" && metadata[MetadataKeyBytecodeFile] != "" {
		return false
	}
	return true
}
