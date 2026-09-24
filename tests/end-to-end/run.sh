#!/usr/bin/env bash
set -e
# set -x

echo "=== PUT test data ==="

package_id=$(curl -XPUT -H "x-api-token: secret" -s http://127.0.0.1:9101/package-version -d @example-001.json | tee /dev/tty)
helm_cluster_id=$(curl -XPUT -H "x-api-token: secret" -s http://127.0.0.1:9101/helm-cluster -d @example-003.json | tee /dev/tty)

echo "=== GET test data ==="
curl -X GET -H "x-api-token: secret" -s http://127.0.0.1:9101/package-version -d "$package_id" | grep debian
curl -X GET -H "x-api-token: secret" -s http://127.0.0.1:9101/helm-cluster -d "$helm_cluster_id" | grep minikube
curl -X GET -s http://127.0.0.1:9101/metrics | grep 'package_version'
curl -X GET -s http://127.0.0.1:9101/metrics | grep 'kubernetes_cluster'

echo "Done"
