#!/bin/bash

# Copyright(c) 2022 Intel Corporation.
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

set -e

cleanup() {
	echo
	echo "*****************************************************"
	echo "*                     Cleanup                       *"
	echo "*****************************************************"
	echo
	echo "Delete remaining uds sockets"
	rm -f /tmp/afxdp/*
}

build() {
	echo
	echo "*****************************************************"
	echo "*          Install and Build Go-Fuzz                *"
	echo "*****************************************************"
	echo
	echo "installing go-fuzz"
	go get github.com/dvyukov/go-fuzz/go-fuzz@e577bee5275c42f9a3ff598ade77fe9806191345 github.com/dvyukov/go-fuzz/go-fuzz-build@e577bee5275c42f9a3ff598ade77fe9806191345
	echo
	echo "building test app"
	go-fuzz-build
	echo
}

run() {
	echo
	echo "*****************************************************"
	echo "*                Run Fuzz Test                      *"
	echo "*****************************************************"
	echo
	echo "running tests"
	go-fuzz -bin=./uds-fuzz.zip -workdir ./outputUDS -dumpcover -func Fuzz
}

cleanup
build
run
trap cleanup EXIT
