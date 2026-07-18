#!/bin/bash
set -e

cd "$(dirname "$0")/.."
echo "Building kspeeder-lite..."
go build -ldflags="-s -w" -o bin/kspeeder-lite ./cmd/kspeeder-lite/
echo "Build complete: bin/kspeeder-lite"
