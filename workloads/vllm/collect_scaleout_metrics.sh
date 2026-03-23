#!/bin/bash
set -euo pipefail

# Reproducible metrics capture for 1->2 vLLM scale-out on EKS GPU nodes.
# Outputs:
#   - summary CSV with readiness and latency stats
#   - raw latency samples
#   - pull-related Kubernetes events
#
# Usage:
#   ./workloads/vllm/collect_scaleout_metrics.sh [results-dir]
#
# Optional env:
#   DEPLOYMENT_NAME=vllm-prebaked
#   SERVICE_NAME=vllm-prebaked
#   NAMESPACE=default
#   LATENCY_SAMPLES=40
#   REQUEST_PATH=/v1/models

NAMESPACE="${NAMESPACE:-default}"
DEPLOYMENT_NAME="${DEPLOYMENT_NAME:-vllm-prebaked}"
SERVICE_NAME="${SERVICE_NAME:-vllm-prebaked}"
LATENCY_SAMPLES="${LATENCY_SAMPLES:-40}"
REQUEST_PATH="${REQUEST_PATH:-/v1/models}"
RESULTS_DIR="${1:-workloads/vllm/results/$(date +%Y%m%d_%H%M%S)}"

mkdir -p "$RESULTS_DIR"

SUMMARY_CSV="$RESULTS_DIR/summary.csv"
RAW_CSV="$RESULTS_DIR/raw_latency.csv"
PULL_EVENTS_TXT="$RESULTS_DIR/pull_events.txt"
POD_PLACEMENT_TXT="$RESULTS_DIR/pod_placement.txt"

echo "metric,value" > "$SUMMARY_CSV"
echo "phase,request_idx,time_seconds" > "$RAW_CSV"

record_metric() {
  echo "$1,$2" >> "$SUMMARY_CSV"
}

calc_percentile() {
  local file="$1"
  local p="$2"
  awk -v p="$p" '
    {a[NR]=$1}
    END{
      if (NR==0) {print "0"; exit}
      n=NR
      pos=(p/100.0)*(n-1)+1
      lo=int(pos)
      hi=(lo<n)?lo+1:lo
      frac=pos-lo
      # assume input already sorted
      val=a[lo] + frac*(a[hi]-a[lo])
      printf "%.6f\n", val
    }
  ' "$file"
}

sample_latency() {
  local phase="$1"
  local endpoint="$2"
  local tmp="$RESULTS_DIR/${phase}_times.tmp"
  : > "$tmp"

  for i in $(seq 1 "$LATENCY_SAMPLES"); do
    t="$(curl -sS -o /dev/null -w '%{time_total}\n' "http://${endpoint}${REQUEST_PATH}")"
    echo "$phase,$i,$t" >> "$RAW_CSV"
    echo "$t" >> "$tmp"
  done

  sort -n "$tmp" -o "$tmp"
  mean="$(awk '{s+=$1} END{if(NR==0){print 0}else{printf "%.6f\n", s/NR}}' "$tmp")"
  p50="$(calc_percentile "$tmp" 50)"
  p95="$(calc_percentile "$tmp" 95)"
  maxv="$(awk 'END{if(NR==0){print 0}else{printf "%.6f\n",$1}}' "$tmp")"

  record_metric "${phase}_latency_mean_sec" "$mean"
  record_metric "${phase}_latency_p50_sec" "$p50"
  record_metric "${phase}_latency_p95_sec" "$p95"
  record_metric "${phase}_latency_max_sec" "$maxv"
}

echo "Applying vLLM manifests..."
t_apply_start="$(date +%s)"
kubectl apply -f workloads/vllm/vllm-deployment.yaml >/dev/null
kubectl apply -f workloads/vllm/vllm-service.yaml >/dev/null

echo "Waiting for replica=1 readiness..."
kubectl rollout status "deployment/${DEPLOYMENT_NAME}" -n "$NAMESPACE" --timeout=30m >/dev/null
t_r1_ready="$(date +%s)"
record_metric "replica1_ready_seconds_from_apply" "$((t_r1_ready - t_apply_start))"

echo "Getting load balancer endpoint..."
LB_ENDPOINT=""
for _ in $(seq 1 90); do
  LB_ENDPOINT="$(kubectl get svc "$SERVICE_NAME" -n "$NAMESPACE" -o jsonpath='{.status.loadBalancer.ingress[0].hostname}' 2>/dev/null || true)"
  if [[ -n "$LB_ENDPOINT" ]]; then
    break
  fi
  sleep 5
done

if [[ -z "$LB_ENDPOINT" ]]; then
  echo "Load balancer hostname not found. Check service provisioning." >&2
  exit 1
fi

record_metric "lb_endpoint" "$LB_ENDPOINT"

echo "Sampling latency before scale-out (1 replica)..."
sample_latency "pre_scale" "$LB_ENDPOINT"

echo "Scaling deployment to 2 replicas..."
t_scale_cmd="$(date +%s)"
kubectl scale deployment "$DEPLOYMENT_NAME" -n "$NAMESPACE" --replicas=2 >/dev/null
kubectl rollout status "deployment/${DEPLOYMENT_NAME}" -n "$NAMESPACE" --timeout=30m >/dev/null
t_r2_ready="$(date +%s)"
record_metric "replica2_ready_seconds_from_scale_cmd" "$((t_r2_ready - t_scale_cmd))"

echo "Capturing pod placement..."
kubectl get pods -n "$NAMESPACE" -l "app=${DEPLOYMENT_NAME}" -o wide > "$POD_PLACEMENT_TXT"

echo "Sampling latency after scale-out (2 replicas)..."
sample_latency "post_scale" "$LB_ENDPOINT"

echo "Collecting pull-related events..."
kubectl get events -n "$NAMESPACE" --sort-by=.lastTimestamp | rg "Pulling|Pulled|Created|Started|${DEPLOYMENT_NAME}" > "$PULL_EVENTS_TXT" || true

record_metric "results_dir" "$RESULTS_DIR"
echo "Done. Results written to: $RESULTS_DIR"
