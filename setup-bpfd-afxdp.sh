#!/bin/bash
make image
docker pull quay.io/mtahhan/cndp-map-pinning
docker pull quay.io/mtahhan/xsk_def_xdp_prog
make setup-multus
make KIND_CLUSTER_NAME=bpfd-deployment kind-label-cp
make KIND_CLUSTER_NAME=bpfd-deployment IMAGE=quay.io/mtahhan/cndp-map-pinning kind-load-custom-image
make KIND_CLUSTER_NAME=bpfd-deployment IMAGE=quay.io/mtahhan/xsk_def_xdp_prog kind-load-custom-image
make KIND_CLUSTER_NAME=bpfd-deployment kind-deploy-bpfd
kubectl create -f examples/nad_with_syncer.yaml
kubectl create -f examples/cndp-0-0.yaml
