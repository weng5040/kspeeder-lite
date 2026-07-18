#!/bin/sh
set -e

# Pass through --help or -h directly
for arg in "$@"; do
    case "$arg" in
        --help|-h|help)
            exec /app/pullfusion "$@"
            ;;
    esac
done

CONFIG="${PF_CONFIG:-/config/nodes.yaml}"

if [ ! -f "$CONFIG" ]; then
    echo "Config file not found at $CONFIG, using built-in defaults"
    CONFIG="/app/configs/nodes.sample.yaml"
fi

exec /app/pullfusion -config "$CONFIG" "$@"
