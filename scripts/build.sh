#!/bin/bash
set -e

cd "$(dirname "$0")/.."
echo "Building pullfusion..."
go build -ldflags="-s -w" -o bin/pullfusion ./cmd/pullfusion/
echo "Build complete: bin/pullfusion"
