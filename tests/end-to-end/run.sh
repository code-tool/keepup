#!/usr/bin/env bash
set -e
# set -x

echo "=== PUT test data ==="

curl -XPUT -H "x-api-token: secret" http://127.0.0.1:9101/package-version -d @example-001.json
curl -XPUT -H "x-api-token: secret" http://127.0.0.1:9101/os-release -d @example-002.json
curl -XPUT -H "x-api-token: secret" http://127.0.0.1:9101/helm-cluster -d @example-003.json

echo "=== GET test data ==="
curl -X GET -H "x-api-token: secret" -s http://127.0.0.1:9101/package-version -d '{"id":"91015d87-2c51-5601-b337-1414f2b5496a"}' | grep debian
curl -X GET -H "x-api-token: secret" -s http://127.0.0.1:9101/os-release -d '{"id":"8b00021e-af61-546e-a0c1-1038bc422d39"}' | grep bullseye
curl -X GET -H "x-api-token: secret" -s http://127.0.0.1:9101/helm-cluster -d '{"id":"688c14fe-9b83-5887-ba6c-f4fa310adc63"}' | grep minikube
curl -X GET -s http://127.0.0.1:9101/metrics | grep 'os'
curl -X GET -s http://127.0.0.1:9101/metrics | grep 'package_version'
curl -X GET -s http://127.0.0.1:9101/metrics | grep 'kubernetes_cluster'

echo "Done"
