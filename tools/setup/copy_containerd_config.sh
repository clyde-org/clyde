#!/bin/bash

# 1. Configuration
CLUSTER_NAME="clyde-cluster-aws"
NODEGROUP_NAME="clyde-on-demand-fleet"
REGION="eu-west-2"
KEY_NAME="~/.ssh/sc-ssh-key.pem" # Assuming it's in the current folder
TIMESTAMP=$(date +%Y%m%d_%H%M%S)

# 2. Fetch IPs
echo "Fetching node IPs..."
IPS=$(aws ec2 describe-instances \
    --region $REGION \
    --filters "Name=tag:eks:cluster-name,Values=$CLUSTER_NAME" \
    "Name=tag:eks:nodegroup-name,Values=$NODEGROUP_NAME" \
    "Name=instance-state-name,Values=running" \
    --query "Reservations[*].Instances[*].PublicIpAddress" \
    --output text)

if [ -z "$IPS" ]; then
    echo "❌ No nodes found. Check your filters."
    exit 1
fi

# 3. Process Nodes
for IP in $IPS; do
    echo ">>> Deploying to: $IP"

    # A. Create a timestamped backup of the current config
    echo "Creating backup: /etc/containerd/config.toml.bak_$TIMESTAMP"
    ssh -i "$KEY_NAME" -o StrictHostKeyChecking=no ubuntu@$IP \
        "sudo cp /etc/containerd/config.toml /etc/containerd/config.toml.bak_$TIMESTAMP"

    # B. Upload the new config
    scp -i "$KEY_NAME" -o StrictHostKeyChecking=no ./config.toml ubuntu@$IP:/tmp/config.toml

    # C. Move, Restart, and Verify
    ssh -i "$KEY_NAME" -o StrictHostKeyChecking=no ubuntu@$IP "
        sudo mv /tmp/config.toml /etc/containerd/config.toml && \
        sudo systemctl restart containerd && \
        sudo systemctl is-active containerd
    "

    if [ $? -eq 0 ]; then
        echo "✅ Success: Containerd is active on $IP"
    else
        echo "❌ Error: Containerd failed on $IP. To revert, run:"
        echo "   ssh ubuntu@$IP 'sudo cp /etc/containerd/config.toml.bak_$TIMESTAMP /etc/containerd/config.toml && sudo systemctl restart containerd'"
    fi
    echo "----------------------------------------------------"
done
