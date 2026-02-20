#!/bin/bash

# 1. Configuration
CLUSTER_NAME="clyde-cluster-aws"
NODEGROUP_NAME="clyde-on-demand-fleet"
REGION="eu-west-2"

# 2. Dynamically fetch IDs
echo "🔍 Fetching Node IDs for $CLUSTER_NAME..."
INSTANCE_IDS=$(aws ec2 describe-instances --region $REGION \
    --filters "Name=tag:eks:cluster-name,Values=$CLUSTER_NAME" \
    "Name=tag:eks:nodegroup-name,Values=$NODEGROUP_NAME" \
    "Name=instance-state-name,Values=running" \
    --query "Reservations[].Instances[].InstanceId" --output text)

if [ -z "$INSTANCE_IDS" ]; then
    echo "❌ No nodes found."
    exit 1
fi

echo "🚀 Running 'crictl images' on nodes: $INSTANCE_IDS"

# 3. Send command to all IDs at once
COMMAND_ID=$(aws ssm send-command \
    --region $REGION \
    --document-name "AWS-RunShellScript" \
    --instance-ids $INSTANCE_IDS \
    --parameters 'commands=["sudo /usr/local/bin/crictl images"]' \
    --query "Command.CommandId" --output text)

echo "⏳ Waiting for execution (Command ID: $COMMAND_ID)..."
sleep 7

# 4. Fetch and display results in a table
# This specifically maps each InstanceId to its command output
aws ssm list-command-invocations \
    --command-id "$COMMAND_ID" \
    --details \
    --query "CommandInvocations[*].{Instance:InstanceId, Status:Status, Images:CommandPlugins[0].Output}" \
    --output table
