#!/usr/bin/env bash
set -euo pipefail

# Root directory for the project (default: current directory)
ROOT_DIR="${1:-.}"

echo "Creating project structure under: $ROOT_DIR"

# List of directories
dirs=(
  "charts/clyde/monitoring"
  "charts/clyde/templates"
  "dist/clyde_linux_amd64"
  "dist/clyde_linux_arm"
  "dist/clyde_linux_arm64"
  "docs/apull_docs"
  "docs/img"
  "internal/buffer"
  "internal/channel"
  "internal/cleanup"
  "internal/mux"
  "internal/web/templates"
  "logs"
  "mermaid"
  "pkg/hf"
  "pkg/metrics"
  "pkg/mux"
  "pkg/oci/testdata/blobs/sha256"
  "pkg/pip"
  "pkg/registry"
  "pkg/routing"
  "pkg/state"
  "secrets"
  "seeding/__pycache__"
  "sir_apull/docker"
  "sir_apull/example"
  "workloads/clyde_apull/certs"
  "workloads/clyde_apull/Llama-2-7b-chat-hf"
  "workloads/clyde_apull/Llama-3.2-1B/original"
  "workloads/clyde_apull/logs"
  "workloads/dockerfiles/amd64"
  "workloads/dockerfiles/arm64"
  "workloads/exp-clyde"
  "workloads/exp-eight-nodes/data"
  "workloads/exp-five-nodes"
  "workloads/hq-16-nodes/data"
  "workloads/pip"
  "workloads/workloads"
  "test/e2e/testdata"
  "tools"
)

# Create directories
for d in "${dirs[@]}"; do
  mkdir -p "$ROOT_DIR/$d"
done

# Add .gitkeep to empty dirs so they're tracked
find "$ROOT_DIR" -type d -empty -exec touch {}/.gitkeep \;

echo "✅ Project structure created successfully."
