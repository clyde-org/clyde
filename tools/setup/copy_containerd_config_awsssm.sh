#!/bin/bash

CLUSTER_NAME="clyde-cluster-aws"
NODEGROUP_NAME="clyde-on-demand-fleet"
REGION="eu-west-2"

# 1. Fetch Instance IDs (instead of IPs)
echo "Fetching Instance IDs..."
INSTANCE_IDS=$(aws ec2 describe-instances \
    --region $REGION \
    --filters "Name=tag:eks:cluster-name,Values=$CLUSTER_NAME" \
    "Name=tag:eks:nodegroup-name,Values=$NODEGROUP_NAME" \
    "Name=instance-state-name,Values=running" \
    --query "Reservations[*].Instances[*].InstanceId" \
    --output text)

if [ -z "$INSTANCE_IDS" ]; then
    echo "❌ No nodes found."
    exit 1
fi

# 2. Upload and Apply via SSM
# Note: We encode the file to base64 to avoid formatting issues during the jump
CONFIG_BASE64=$(base64 -i ./config.toml)

echo ">>> Deploying config to all nodes via SSM..."
aws ssm send-command \
    --document-name "AWS-RunShellScript" \
    --instance-ids $INSTANCE_IDS \
    --parameters 'commands=[
        "sudo cp /etc/containerd/config.toml /etc/containerd/config.toml.bak_$(date +%Y%m%d)",
        "echo '"$CONFIG_BASE64"' | base64 -d | sudo tee /etc/containerd/config.toml > /dev/null",
        "sudo systemctl restart containerd",
        "sudo systemctl is-active containerd"
    ]' \
    --region $REGION
