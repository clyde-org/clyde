#!/bin/bash

# 1. Configuration
CLUSTER_NAME="clyde-cluster-aws"
NODEGROUP_NAME="clyde-on-demand-fleet"
REGION="eu-west-2"

# 2. Fetch Instance IDs
echo "🔍 Fetching Instance IDs..."
INSTANCE_IDS=$(aws ec2 describe-instances --region $REGION \
    --filters "Name=tag:eks:cluster-name,Values=$CLUSTER_NAME" \
    "Name=tag:eks:nodegroup-name,Values=$NODEGROUP_NAME" \
    "Name=instance-state-name,Values=running" \
    --query "Reservations[].Instances[].InstanceId" --output text)

if [ -z "$INSTANCE_IDS" ]; then
    echo "❌ No nodes found."
    exit 1
fi

echo "🚀 Installing tools on all nodes..."

# 3. Executing with Absolute Paths and ARM64 hardcoded for Graviton
COMMAND_ID=$(aws ssm send-command \
    --region $REGION \
    --document-name "AWS-RunShellScript" \
    --instance-ids $INSTANCE_IDS \
    --parameters 'commands=[
        "cd /tmp",
        "echo Installing nerdctl...",
        "curl -Lso nerdctl.tar.gz https://github.com/containerd/nerdctl/releases/download/v1.7.3/nerdctl-1.7.3-linux-arm64.tar.gz",
        "sudo tar Cxzf /usr/local/bin nerdctl.tar.gz",
        "echo Installing crictl...",
        "curl -Lso crictl.tar.gz https://github.com/kubernetes-sigs/cri-tools/releases/download/v1.29.0/crictl-v1.29.0-linux-arm64.tar.gz",
        "sudo tar Cxzf /usr/local/bin crictl.tar.gz",
        "sudo chmod +x /usr/local/bin/nerdctl /usr/local/bin/crictl",
        "/usr/local/bin/nerdctl --version",
        "/usr/local/bin/crictl --version"
    ]' \
    --query "Command.CommandId" --output text)

echo "✅ Sent! Command ID: $COMMAND_ID"
