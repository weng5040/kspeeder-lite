#!/bin/bash
set -e

cd "$(dirname "$0")/.."
echo "Running tests..."
go test ./... -v -cover -coverprofile=coverage.out
echo "Coverage report: coverage.out"
