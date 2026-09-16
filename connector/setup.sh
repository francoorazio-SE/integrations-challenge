#!/usr/bin/env bash
#
# connector/setup.sh - builds the Go connector once, before the scenario
# series. run.sh itself just execs the resulting binary and needs no network
# access beyond the two local servers.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

go -C "$REPO_ROOT" build -o "$SCRIPT_DIR/bin/connector" ./connector

echo "setup.sh: built $SCRIPT_DIR/bin/connector"
