#!/bin/bash

# 1. Configuration
CLUSTER_NAME="clyde-cluster-aws"
NODEGROUP_NAME="clyde-on-demand-fleet"
REGION="eu-west-2"
KEY_NAME="$HOME/.ssh/sc-ssh-key.pem"

[[ -z "$1" ]] && echo "Usage: $0 <image-name>" && exit 1
IMAGE=$1

# 2. Fetch IPs
IPS=$(aws ec2 describe-instances --region $REGION \
    --filters "Name=tag:eks:cluster-name,Values=$CLUSTER_NAME" "Name=tag:eks:nodegroup-name,Values=$NODEGROUP_NAME" "Name=instance-state-name,Values=running" \
    --query "Reservations[].Instances[].PublicIpAddress" --output text)

echo "🚀 Nuking $IMAGE and dangling layers on $(echo $IPS | wc -w) nodes..."

# 3. Parallel Cleanup
for IP in $IPS; do
    (
        ssh -i "$KEY_NAME" -n -o StrictHostKeyChecking=no -o ConnectTimeout=5 ubuntu@"$IP" "
            # Set endpoint to silence warnings
            sudo crictl config --runtime-endpoint unix:///run/containerd/containerd.sock > /dev/null 2>&1
            
            # 1. Remove the specific image (handles the named tag)
            sudo crictl rmi $IMAGE > /dev/null 2>&1
            
            # 2. Prune dangling images (handles the 13GB <none> ghost)
            sudo crictl rmi --prune > /dev/null 2>&1
        " && echo "✅ $IP" || echo "❌ $IP"
    ) &
done

wait
echo "🏁 Done."
