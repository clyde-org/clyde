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
  "sir_benchmark/clyde_apull/certs"
  "sir_benchmark/clyde_apull/Llama-2-7b-chat-hf"
  "sir_benchmark/clyde_apull/Llama-3.2-1B/original"
  "sir_benchmark/clyde_apull/logs"
  "sir_benchmark/dockerfiles/amd64"
  "sir_benchmark/dockerfiles/arm64"
  "sir_benchmark/exp-clyde"
  "sir_benchmark/exp-eight-nodes/data"
  "sir_benchmark/exp-five-nodes"
  "sir_benchmark/hq-16-nodes/data"
  "sir_benchmark/pip"
  "sir_benchmark/workloads"
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
