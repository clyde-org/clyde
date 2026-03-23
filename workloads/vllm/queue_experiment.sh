#!/bin/bash
set -euo pipefail

# Run one full load+scale experiment and capture queue-over-time artifacts.
#
# Usage:
#   ./workloads/vllm/queue_experiment.sh <scenario-label> [results-base-dir]
#
# Example:
#   ./workloads/vllm/queue_experiment.sh baseline
#   ./workloads/vllm/queue_experiment.sh clyde
#
# Optional env:
#   NAMESPACE=default
#   DEPLOYMENT_NAME=vllm-prebaked
#   SERVICE_NAME=vllm-prebaked
#   REQUEST_PATH=/v1/models
#   LOAD_DURATION_SEC=300
#   LOAD_RPS=3
#   LOAD_CONCURRENCY=32
#   SCALE_AT_SEC=60

SCENARIO="${1:-}"
if [[ -z "$SCENARIO" ]]; then
  echo "Usage: $0 <scenario-label> [results-base-dir]"
  exit 1
fi

NAMESPACE="${NAMESPACE:-default}"
DEPLOYMENT_NAME="${DEPLOYMENT_NAME:-vllm-prebaked}"
SERVICE_NAME="${SERVICE_NAME:-vllm-prebaked}"
REQUEST_PATH="${REQUEST_PATH:-/v1/models}"
LOAD_DURATION_SEC="${LOAD_DURATION_SEC:-300}"
LOAD_RPS="${LOAD_RPS:-3}"
LOAD_CONCURRENCY="${LOAD_CONCURRENCY:-32}"
SCALE_AT_SEC="${SCALE_AT_SEC:-60}"
RESULTS_BASE="${2:-workloads/vllm/results}"
OUT_DIR="${RESULTS_BASE}/${SCENARIO}_$(date +%Y%m%d_%H%M%S)"

mkdir -p "$OUT_DIR"
SUMMARY_CSV="$OUT_DIR/summary.csv"
echo "metric,value" > "$SUMMARY_CSV"

record_metric() {
  echo "$1,$2" >> "$SUMMARY_CSV"
}

echo "Applying deployment/service..."
kubectl apply -f workloads/vllm/vllm-deployment.yaml >/dev/null
kubectl apply -f workloads/vllm/vllm-service.yaml >/dev/null

echo "Ensuring start state is 1 replica..."
kubectl scale deployment "$DEPLOYMENT_NAME" -n "$NAMESPACE" --replicas=1 >/dev/null
t_apply="$(date +%s)"
kubectl rollout status deployment/"$DEPLOYMENT_NAME" -n "$NAMESPACE" --timeout=30m >/dev/null
t_r1="$(date +%s)"
record_metric "scenario" "$SCENARIO"
record_metric "replica1_ready_seconds_from_apply" "$((t_r1 - t_apply))"

LB_ENDPOINT=""
for _ in $(seq 1 120); do
  LB_ENDPOINT="$(kubectl get svc "$SERVICE_NAME" -n "$NAMESPACE" -o jsonpath='{.status.loadBalancer.ingress[0].hostname}' 2>/dev/null || true)"
  if [[ -n "$LB_ENDPOINT" ]]; then
    break
  fi
  sleep 5
done
if [[ -z "$LB_ENDPOINT" ]]; then
  echo "Could not resolve load balancer endpoint"
  exit 1
fi
record_metric "lb_endpoint" "$LB_ENDPOINT"

echo "Starting load run for ${LOAD_DURATION_SEC}s at ${LOAD_RPS} rps..."
python3 workloads/vllm/load_runner.py \
  --endpoint "http://${LB_ENDPOINT}" \
  --path "$REQUEST_PATH" \
  --duration-sec "$LOAD_DURATION_SEC" \
  --rps "$LOAD_RPS" \
  --concurrency "$LOAD_CONCURRENCY" \
  --output-dir "$OUT_DIR" &
LOAD_PID=$!

echo "Will scale to 2 replicas at t=${SCALE_AT_SEC}s..."
sleep "$SCALE_AT_SEC"
t_scale="$(date +%s)"
kubectl scale deployment "$DEPLOYMENT_NAME" -n "$NAMESPACE" --replicas=2 >/dev/null
kubectl rollout status deployment/"$DEPLOYMENT_NAME" -n "$NAMESPACE" --timeout=30m >/dev/null
t_r2="$(date +%s)"
record_metric "replica2_ready_seconds_from_scale_cmd" "$((t_r2 - t_scale))"
record_metric "scale_at_sec" "$SCALE_AT_SEC"

echo "Waiting for load process..."
wait "$LOAD_PID"

echo "Capturing pod placement and events..."
kubectl get pods -n "$NAMESPACE" -l app="$DEPLOYMENT_NAME" -o wide > "$OUT_DIR/pod_placement.txt"
kubectl get events -n "$NAMESPACE" --sort-by=.lastTimestamp | rg "Pulling|Pulled|Created|Started|${DEPLOYMENT_NAME}" > "$OUT_DIR/pull_events.txt" || true

record_metric "results_dir" "$OUT_DIR"
echo "Done: $OUT_DIR"
