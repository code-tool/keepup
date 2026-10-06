#!/usr/bin/env bash
set -e
# set -x

echo "=== PUT test data ==="

PACKAGE_ID=$(curl -XPUT -H "x-api-token: secret" -s http://127.0.0.1:9101/package-version -d @example-001.json)
HELM_CLUSTER_ID=$(curl -XPUT -H "x-api-token: secret" -s http://127.0.0.1:9101/helm-cluster -d @example-003.json)
echo $PACKAGE_ID
echo $HELM_CLUSTER_ID

echo "=== GET test data ==="
curl -X GET -H "x-api-token: secret" -s http://127.0.0.1:9101/package-version -d "$PACKAGE_ID" | grep debian
curl -X GET -H "x-api-token: secret" -s http://127.0.0.1:9101/helm-cluster -d "$HELM_CLUSTER_ID" | grep minikube
curl -X GET -s http://127.0.0.1:9101/metrics | grep 'package_version'
curl -X GET -s http://127.0.0.1:9101/metrics | grep 'kubernetes_cluster'

echo "Done"
