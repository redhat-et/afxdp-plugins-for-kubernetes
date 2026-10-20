#!/bin/bash
set -e

cleanup() {
	echo
	echo "*****************************************************"
	echo "*                     Cleanup                       *"
	echo "*****************************************************"
	echo
	echo "delete remaining uds sockets"
	rm -f /tmp/afxdp/*
	echo 
	echo "delete config temp directory"
	rm -rf config/ 
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
	go-fuzz -bin=./deviceplugin-fuzz.zip -workdir ./outputConfig -dumpcover -func Fuzz
}

cleanup
build
run
trap cleanup EXIT
