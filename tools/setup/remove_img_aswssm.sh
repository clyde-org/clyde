#!/bin/bash

# 1. Configuration
CLUSTER_NAME="clyde-cluster-aws"
NODEGROUP_NAME="clyde-on-demand-fleet"
REGION="eu-west-2"

[[ -z "$1" ]] && echo "Usage: $0 <image-name>" && exit 1
IMAGE=$1

# 2. Fetch Instance IDs (Required for SSM)
echo "🔍 Fetching Instance IDs for cluster: $CLUSTER_NAME..."
INSTANCE_IDS=$(aws ec2 describe-instances --region $REGION \
    --filters "Name=tag:eks:cluster-name,Values=$CLUSTER_NAME" \
    "Name=tag:eks:nodegroup-name,Values=$NODEGROUP_NAME" \
    "Name=instance-state-name,Values=running" \
    --query "Reservations[].Instances[].InstanceId" --output text)

if [ -z "$INSTANCE_IDS" ]; then
    echo "❌ No running nodes found. Check your cluster name and filters."
    exit 1
fi

NODE_COUNT=$(echo $INSTANCE_IDS | wc -w)
echo "🚀 Nuking $IMAGE and dangling layers on $NODE_COUNT nodes via SSM..."

# 3. Execute via SSM
COMMAND_ID=$(aws ssm send-command \
    --region $REGION \
    --document-name "AWS-RunShellScript" \
    --instance-ids $INSTANCE_IDS \
    --comment "Nuking image $IMAGE and pruning dangling layers" \
    --parameters 'commands=[
        "sudo crictl config --runtime-endpoint unix:///run/containerd/containerd.sock > /dev/null 2>&1",
        "sudo crictl rmi '"$IMAGE"' > /dev/null 2>&1 || true",
        "sudo crictl rmi --prune > /dev/null 2>&1"
    ]' \
    --query "Command.CommandId" --output text)

echo "✅ Command sent! Command ID: $COMMAND_ID"
echo "⏳ Monitoring progress... (Ctrl+C to stop monitoring, command will continue)"

# 4. Optional: Monitor progress
aws ssm list-command-invocations \
    --command-id "$COMMAND_ID" \
    --details \
    --query "CommandInvocations[*].{Instance:InstanceId,Status:Status}" \
    --output table
