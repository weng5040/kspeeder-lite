#!/bin/bash
# pullfusion 开发同步脚本
# 将本地代码同步到开发服务器并编译

set -e

SERVER="root@172.16.7.29"
PORT="8022"
KEY="$HOME/.ssh/pullfusion-key"
REMOTE_DIR="/opt/pullfusion"
SSH_OPTS="-o StrictHostKeyChecking=no -o Port=${PORT} -i ${KEY}"

cd "$(dirname "$0")/.."

echo "=== Syncing code to ${SERVER} ==="
tar czf - . --exclude='go.sum' --exclude='bin' --exclude='.git' | \
  ssh ${SSH_OPTS} ${SERVER} "cd ${REMOTE_DIR} && tar xzf -"

echo "=== Building on server ==="
ssh ${SSH_OPTS} ${SERVER} "export PATH=\$PATH:/usr/local/go/bin && cd ${REMOTE_DIR} && go mod tidy && go build -ldflags='-s -w' -o bin/pullfusion ./cmd/pullfusion/ && echo 'Build successful: ' && ls -lh bin/pullfusion"

echo "=== Running tests ==="
ssh ${SSH_OPTS} ${SERVER} "export PATH=\$PATH:/usr/local/go/bin && cd ${REMOTE_DIR} && go test ./... -v -count=1 2>&1"
