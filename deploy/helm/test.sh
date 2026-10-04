#!/usr/bin/env bash
# Installs the chart in a throwaway kind cluster with the locally built image
# and checks that the pod becomes ready and serves the watched targets.
# Needs: docker, kind, kubectl, helm.
set -euo pipefail

root="$(cd "$(dirname "$0")/../.." && pwd)"
cluster=permwatch-test

cleanup() { kind delete cluster --name "$cluster" >/dev/null 2>&1 || true; }
trap cleanup EXIT

docker build -q -f "$root/deploy/Dockerfile" -t permwatch:dev "$root" >/dev/null
kind create cluster --name "$cluster" --wait 120s >/dev/null 2>&1
kind load docker-image permwatch:dev --name "$cluster" >/dev/null 2>&1

helm install permwatch "$root/deploy/helm/permwatch" -f "$root/deploy/helm/permwatch/ci-values.yaml" \
  --wait --timeout 180s >/dev/null
kubectl rollout status deployment/permwatch --timeout=120s
kubectl get pods -l app.kubernetes.io/name=permwatch -o wide --no-headers | awk '{print "pod", $1, $2, $3}'
kubectl get pvc --no-headers | awk '{print "pvc", $1, $2}'

kubectl port-forward svc/permwatch 18080:8080 >/dev/null 2>&1 &
forward=$!
sleep 3
echo "readyz $(curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:18080/readyz)"
curl -s http://127.0.0.1:18080/v1/accounts | grep -o '"label":"[^"]*"' | sort -u
curl -s http://127.0.0.1:18080/v1/vaults | grep -o '"label":"[^"]*"' | sort -u
curl -s http://127.0.0.1:18080/metrics | grep -c '^permwatch_' | sed 's/^/permwatch metric lines: /'
kill "$forward" 2>/dev/null || true

kubectl exec deploy/permwatch -- /permwatch 2>&1 | head -1 || true
echo "user: $(kubectl get pod -l app.kubernetes.io/name=permwatch -o jsonpath='{.items[0].spec.securityContext.runAsUser}')"
echo "OK"
