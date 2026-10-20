#!/usr/bin/env bash

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

# End-to-end test of the AF_XDP device plugin and CNI in bpfman mode.
#
# Unlike e2e-test.sh (legacy UDS mode), this test verifies that:
#   - the device plugin loads the XDP program on the pool's netdevs via bpfman
#   - the bpfman CSI driver exposes the pool's XSKMAPs into the pod under
#     /run/xdp/maps/<netdev>/xsks_map
#   - an unprivileged pod (no BPF capabilities) can open the XSKMAP
#
# Prerequisites:
#   - A kind cluster with multus (e.g. `make setup-kind label-kind-nodes setup-multus`)
#   - The plugins built (`make build`) and the DP image built (`make image`)
#   - bpfman deployed on the cluster, deployable by this script in one of:
#       - BPFMAN_OPERATOR_DIR: path to a bpfman-operator checkout
#         (deployed with `kubectl apply -k config/default`)
#       - BPFMAN_MANIFEST: path or URL of a rendered bpfman deployment manifest
#       - or already deployed (detected via the bpfman namespace)
#     NOTE: the CSI metadata-selector volume attributes used by this test
#     require a bpfman build that includes the
#     csi.bpfman.io/programs-by-metadata support (bpfman main or the
#     csi-afxdp-metadata-selector branch of this change set).
#   - Optional: BPFMAN_IMAGE overrides the bpfman daemon image after deploy
#     (for testing locally built bpfman images).

set -e

workdir="."
container_tool=""
kind_cluster="af-xdp-deployment"
bpfman_namespace="bpfman"
bpfman_image=""
bpfman_operator_dir=""
bpfman_manifest=""
keep_bpfman=false
timeout_secs=300

detect_container_engine() {
	echo "*****************************************************"
	echo "*          Checking Container Engine                *"
	echo "*****************************************************"
	if podman -v; then \
	  container_tool="podman"; \
	else \
	  container_tool="docker"; \
	fi

	echo "$container_tool recognised as container engine"
}

bpfman_deployed() {
	kubectl get namespace "$bpfman_namespace" &> /dev/null
}

deploy_bpfman() {
	echo
	echo "*****************************************************"
	echo "*              Deploying bpfman                     *"
	echo "*****************************************************"
	if bpfman_deployed; then
		echo "bpfman is already deployed, skipping deployment"
		return
	fi

	if [ -n "$bpfman_manifest" ]; then
		echo "Deploying bpfman from manifest: $bpfman_manifest"
		kubectl apply -f "$bpfman_manifest"
	elif [ -n "$bpfman_operator_dir" ]; then
		echo "Deploying bpfman from operator directory: $bpfman_operator_dir"
		(cd "$bpfman_operator_dir" && kubectl apply -k config/default)
	else
		echo "bpfman is not deployed and neither BPFMAN_OPERATOR_DIR nor BPFMAN_MANIFEST is set."
		echo "Deploy bpfman first (see https://bpfman.io) or set one of the variables."
		exit 1
	fi

	if ! bpfman_deployed; then
		echo "The bpfman deployment did not create the $bpfman_namespace namespace"
		exit 1
	fi
}

override_bpfman_image() {
	if [ -z "$bpfman_image" ]; then
		return
	fi
	echo
	echo "*****   Overriding the bpfman daemon image   *****"
	echo
	kubectl -n "$bpfman_namespace" set image daemonset/bpfman-daemon bpfman="$bpfman_image"
}

wait_for_daemonset() {
	local what="$1" namespace="$2" daemonset="$3"

	echo "Waiting for $what to be ready"
	if ! kubectl -n "$namespace" rollout status daemonset/"$daemonset" --timeout="${timeout_secs}s"; then
		echo "Error: $what took too long to become ready"
		kubectl -n "$namespace" describe daemonset "$daemonset"
		kubectl -n "$namespace" get pods -o wide
		return 1
	fi
}

wait_for_pod() {
	local pod="$1" counter=0

	while true; do
		local state
		state=$(kubectl get pod "$pod" --no-headers 2> /dev/null | awk '{print $3}')
		if [ "$state" == "Running" ]; then
			echo "Pod $pod is running"
			break
		fi
		counter=$((counter+1))
		if (( counter % 10 == 0 )); then
			echo "Pod $pod state: ${state:-unknown}"
		fi
		if (( counter > timeout_secs/10 )); then
			echo "Error: pod $pod took too long to start"
			kubectl describe pod "$pod"
			return 1
		fi
		sleep 10
	done
}

cleanup() {
	echo
	echo "*****************************************************"
	echo "*                     Cleanup                       *"
	echo "*****************************************************"
	echo "Delete Pod"
	kubectl delete pod --grace-period=0 --ignore-not-found=true afxdp-e2e-bpfman-test &> /dev/null
	echo "Delete Network Attachment Definition"
	kubectl delete --ignore-not-found=true -f $workdir/nad-bpfman.yaml &> /dev/null
	echo "Delete Device Plugin Daemonset"
	kubectl delete --ignore-not-found=true -f $workdir/../deployments/daemonset-kind-bpfman.yaml &> /dev/null
	echo "Delete Test App"
	rm -f ./bpfmanTest &> /dev/null
	if [ "$keep_bpfman" = false ]; then
		echo "Delete bpfman"
		if [ -n "$bpfman_manifest" ]; then
			kubectl delete --ignore-not-found=true -f "$bpfman_manifest" &> /dev/null
		elif [ -n "$bpfman_operator_dir" ]; then
			(cd "$bpfman_operator_dir" && kubectl delete --ignore-not-found=true -k config/default &> /dev/null)
		fi
		kubectl delete --ignore-not-found=true namespace "$bpfman_namespace" &> /dev/null
	fi
}

build() {
	echo
	echo "*****************************************************"
	echo "*               Build and Install                   *"
	echo "*****************************************************"
	echo "***** Test App *****"
	go build -tags netgo -o bpfmanTest ./bpfman/
	echo "***** Test App Image *****"
	$container_tool build -t afxdp-e2e-test -f Dockerfile .
	echo "***** Load images into kind *****"
	kind load --name "$kind_cluster" docker-image afxdp-device-plugin
	kind load --name "$kind_cluster" docker-image afxdp-e2e-test
	echo "***** Network Attachment Definition *****"
	kubectl create -f $workdir/nad-bpfman.yaml
}

run() {
	echo
	echo "*****************************************************"
	echo "*                 Run Test                          *"
	echo "*****************************************************"
	deploy_bpfman
	override_bpfman_image
	wait_for_daemonset "bpfman" "$bpfman_namespace" "bpfman-daemon"

	echo "***** Deploying Device Plugin as daemonset *****"
	kubectl create -f $workdir/../deployments/daemonset-kind-bpfman.yaml
	wait_for_daemonset "AF_XDP Device Plugin" "kube-system" "kube-afxdp-device-plugin-bpfman"

	echo "***** Creating test pod *****"
	kubectl create -f $workdir/pod-bpfman.yaml
	wait_for_pod afxdp-e2e-bpfman-test

	echo
	echo "***** Netdevs attached to pod (ip a) *****"
	echo
	kubectl exec -i afxdp-e2e-bpfman-test -- ip a
	sleep 2
	echo
	echo "***** Pod Env Vars *****"
	echo
	kubectl exec -i afxdp-e2e-bpfman-test -- env
	echo
	echo "***** XSKMAP Test *****"
	echo
	kubectl exec -i afxdp-e2e-bpfman-test --container afxdp -- cat /tmp/bpfmanTest.txt
	echo

	if ! kubectl exec -i afxdp-e2e-bpfman-test --container afxdp -- cat /tmp/bpfmanTest.txt | grep -q "Test App - PASSED"; then
		echo "Error: bpfmanTest did not pass"
		exit 1
	fi
	echo "bpfman e2e test PASSED"
}

display_help() {
	echo "Usage: $0 [option...]"
	echo
	echo "  -h, --help                Print Help (this message) and exit"
	echo "  -k, --keep-bpfman         Do not delete the bpfman deployment during cleanup"
	echo
	echo "Environment variables:"
	echo "  BPFMAN_OPERATOR_DIR       Path to a bpfman-operator checkout used to deploy bpfman"
	echo "  BPFMAN_MANIFEST           Path or URL of a rendered bpfman deployment manifest"
	echo "  BPFMAN_IMAGE              Override the bpfman daemon image after deployment"
	echo "  KIND_CLUSTER              kind cluster name (default: af-xdp-deployment)"
	echo
	exit 0
}

if [ -n "${1-}" ]
then
	while :; do
		case $1 in
			-h|--help)
				display_help
			;;
			-k|--keep-bpfman)
				keep_bpfman=true
			;;
			-?*)
				echo "Unknown argument $1"
				exit 1
			;;
			*) break
		esac
		shift
	done
fi

bpfman_image="${BPFMAN_IMAGE:-}"
bpfman_operator_dir="${BPFMAN_OPERATOR_DIR:-}"
bpfman_manifest="${BPFMAN_MANIFEST:-}"
kind_cluster="${KIND_CLUSTER:-$kind_cluster}"

detect_container_engine
cleanup
build
run
trap cleanup EXIT
