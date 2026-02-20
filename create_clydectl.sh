#!/bin/bash
set -e

BASE_DIR="tools/clydectl"

echo "Creating clydectl directory structure..."

# Top-level
mkdir -p ${BASE_DIR}

# cmd
mkdir -p ${BASE_DIR}/cmd

# internal sub-structure
mkdir -p ${BASE_DIR}/internal/kube
mkdir -p ${BASE_DIR}/internal/seed
mkdir -p ${BASE_DIR}/internal/util

# Create files (only if they don't exist)
touch ${BASE_DIR}/go.mod
touch ${BASE_DIR}/main.go
touch ${BASE_DIR}/README.md

touch ${BASE_DIR}/cmd/root.go
touch ${BASE_DIR}/cmd/deploy.go

touch ${BASE_DIR}/internal/kube/client.go
touch ${BASE_DIR}/internal/kube/nodes.go
touch ${BASE_DIR}/internal/kube/job.go
touch ${BASE_DIR}/internal/kube/daemonset.go

touch ${BASE_DIR}/internal/seed/planner.go
touch ${BASE_DIR}/internal/seed/executor.go

touch ${BASE_DIR}/internal/util/wait.go

echo "clydectl structure created successfully."
echo
echo "Resulting structure:"
tree tools/clydectl

