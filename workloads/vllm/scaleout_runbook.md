# vLLM GPU Scale-Out Runbook (EKS, 2 nodes)

This runbook executes a simple 1->2 replica GPU scale-out test on EKS with a
pre-baked vLLM image and a public load balancer.

## 0) Prerequisites

- AWS CLI, `kubectl`, and `eksctl` installed and authenticated.
- Internet egress from nodes to pull public image/model.
- Defaults already point to a public image/model:
  - image: `vllm/vllm-openai:latest`
  - model: `Qwen/Qwen2.5-7B-Instruct`
- Optional: edit `workloads/vllm/vllm-deployment.yaml` if you want a different model.

## 1) Create 2-node GPU cluster

```bash
eksctl create cluster -f infra/eks/gpu-2node-cluster.yaml
```

Verify:

```bash
kubectl get nodes -o wide
```

Expected: exactly 2 worker nodes, each `g5.xlarge`.

## 2) Enable GPU scheduling

```bash
bash tools/setup/enable_nvidia_device_plugin.sh
```

Verify both nodes expose one GPU:

```bash
kubectl get nodes -o custom-columns=NAME:.metadata.name,GPU:.status.allocatable.nvidia\\.com/gpu
```

## 3) Deploy vLLM (Phase A: one replica)

```bash
kubectl apply -f workloads/vllm/vllm-deployment.yaml
kubectl apply -f workloads/vllm/vllm-service.yaml
kubectl rollout status deployment/vllm-prebaked --timeout=20m
kubectl get pods -l app=vllm-prebaked -o wide
kubectl get svc vllm-prebaked
```

Expected:
- one running pod,
- pod scheduled on a GPU node,
- `EXTERNAL-IP` or LB hostname assigned.

## 4) Scale out to two replicas (Phase B)

```bash
kubectl scale deployment vllm-prebaked --replicas=2
kubectl rollout status deployment/vllm-prebaked --timeout=20m
kubectl get pods -l app=vllm-prebaked -o wide
```

Expected:
- two running pods,
- one pod per node (enforced by anti-affinity).

## 5) Validate load balancing

Set endpoint:

```bash
export VLLM_LB=$(kubectl get svc vllm-prebaked -o jsonpath='{.status.loadBalancer.ingress[0].hostname}')
echo "$VLLM_LB"
```

Send requests:

```bash
for i in $(seq 1 20); do
  curl -s "http://$VLLM_LB/v1/models" >/dev/null
done
```

Check both pods received traffic:

```bash
kubectl logs -l app=vllm-prebaked --since=5m | rg -n "GET|POST|/v1/models|/v1/chat/completions"
```

## 6) Cleanup

```bash
kubectl delete -f workloads/vllm/vllm-service.yaml
kubectl delete -f workloads/vllm/vllm-deployment.yaml
```
