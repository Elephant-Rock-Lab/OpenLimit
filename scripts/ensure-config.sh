#!/usr/bin/env bash
# Auto-copy gateway.example.yaml → gateway.yaml on first run
# Called by: make run
set -euo pipefail

CONFIG="configs/gateway.yaml"
EXAMPLE="configs/gateway.example.yaml"

if [ ! -f "$CONFIG" ]; then
    if [ -f "$EXAMPLE" ]; then
        cp "$EXAMPLE" "$CONFIG"
        echo "⚠️  Created $CONFIG from example. Edit it with your provider keys before running again."
        echo "   Set OPENAI_API_KEY or edit $CONFIG to configure providers."
        exit 0
    else
        echo "❌ Neither $CONFIG nor $EXAMPLE found."
        exit 1
    fi
fi
