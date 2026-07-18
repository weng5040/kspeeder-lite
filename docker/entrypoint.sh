#!/bin/sh
set -e

CONFIG="${KS_CONFIG:-/config/nodes.yaml}"

if [ ! -f "$CONFIG" ]; then
    echo "Config file not found at $CONFIG, using built-in defaults"
    CONFIG="/app/configs/nodes.sample.yaml"
fi

exec /app/kspeeder-lite -config "$CONFIG"
