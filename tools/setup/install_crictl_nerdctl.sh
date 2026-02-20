#!/bin/bash
# Configuration from your previous setup
CLUSTER_NAME="clyde-cluster-aws"
NODEGROUP_NAME="clyde-on-demand-fleet"
REGION="eu-west-2"
KEY_NAME="$HOME/.ssh/sc-ssh-key.pem"

IPS=$(aws ec2 describe-instances --region $REGION \
    --filters "Name=tag:eks:cluster-name,Values=$CLUSTER_NAME" "Name=tag:eks:nodegroup-name,Values=$NODEGROUP_NAME" "Name=instance-state-name,Values=running" \
    --query "Reservations[].Instances[].PublicIpAddress" --output text)

for IP in $IPS; do
    (
        ssh -i "$KEY_NAME" -n -o StrictHostKeyChecking=no ubuntu@"$IP" "
            # Install nerdctl (minimal standalone binary)
            ARCH=\$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')
            curl -Lso nerdctl.tar.gz https://github.com/containerd/nerdctl/releases/download/v1.7.3/nerdctl-1.7.3-linux-\$ARCH.tar.gz
            sudo tar Cxzf /usr/local/bin nerdctl.tar.gz && rm nerdctl.tar.gz
            
            # Install crictl
            curl -Lso crictl.tar.gz https://github.com/kubernetes-sigs/cri-tools/releases/download/v1.29.0/crictl-v1.29.0-linux-\$ARCH.tar.gz
            sudo tar Cxzf /usr/local/bin crictl.tar.gz && rm crictl.tar.gz
        " && echo "✅ $IP: Installed" || echo "❌ $IP: Failed"
    ) &
done
wait
