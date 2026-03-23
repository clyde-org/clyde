# vLLM on EKS GPU (2-node) Quickstart

This folder contains manifests and scripts for a low-cost 2-node EKS GPU
scale-out test using a public vLLM image and model.

## Files

- `vllm-deployment.yaml` - GPU deployment (starts with `replicas: 1`)
- `vllm-service.yaml` - `LoadBalancer` service for external traffic
- `scaleout_runbook.md` - step-by-step 1->2 replica workflow
- `collect_scaleout_metrics.sh` - reproducible readiness + latency capture
- `queue_experiment.sh` - runs one full load+scale scenario and records queue-over-time metrics
- `load_runner.py` - fixed-rate request generator used by queue experiment
- `plot_queue_experiment.py` - compares baseline vs Clyde queue/latency time-series

## Minimal command flow

1. Create cluster:

```bash
eksctl create cluster -f infra/eks/gpu-2node-cluster.yaml
```

2. Enable GPU resources:

```bash
bash tools/setup/enable_nvidia_device_plugin.sh
```

3. Optional: switch image/model (defaults already public):

```bash
# Defaults in the manifest:
# image: vllm/vllm-openai:latest
# model: Qwen/Qwen2.5-7B-Instruct
```

4. Deploy and run scale-out test:

```bash
bash workloads/vllm/collect_scaleout_metrics.sh
```

The script writes results under `workloads/vllm/results/<timestamp>/`.

## Queue-over-time experiment (Baseline vs Clyde)

Run one scenario at a time:

```bash
# Scenario 1
bash workloads/vllm/queue_experiment.sh baseline

# Scenario 2
bash workloads/vllm/queue_experiment.sh clyde
```

Each run writes:
- `requests.csv` (per request sent/completed timestamps)
- `per_second.csv` (sent/completed/backlog and per-second p95 latency)
- `summary.csv` (ready times + endpoint)
- `pod_placement.txt`, `pull_events.txt`

Then compare both runs and generate paper plots:

```bash
python3 workloads/vllm/plot_queue_experiment.py \
  --baseline-dir workloads/vllm/results/<baseline_run_dir> \
  --clyde-dir workloads/vllm/results/<clyde_run_dir> \
  --outdir workloads/vllm/results/plots
```

Generated comparison artifacts:
- `queue_vs_time.png`
- `latency_vs_time.png`
- `queue_latency_summary.csv`
