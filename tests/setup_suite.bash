#!/bin/bash

set -eu

function setup_suite {
  export BATS_TEST_TIMEOUT=120
  # Define the name of the kind cluster
  export CLUSTER_NAME="ccm-kind"
  # Build the cloud-provider-kind
  cd "$BATS_TEST_DIRNAME"/..
 
  mkdir -p _artifacts
  rm -rf _artifacts/*
  # create cluster
  cat <<EOF | kind create cluster \
  --name $CLUSTER_NAME           \
  -v7 --wait 1m --retain --config=-
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
- role: worker
- role: worker
EOF

  make
  nohup bin/cloud-provider-kind -v 2 --enable-log-dumping --logs-dir ./_artifacts > ./_artifacts/ccm-kind.log 2>&1 &

  # test depend on external connectivity that can be very flaky
  sleep 5
}

function teardown_suite {
    kind export logs "$BATS_TEST_DIRNAME"/../_artifacts --name "$CLUSTER_NAME"
    kind delete cluster --name "$CLUSTER_NAME"
}