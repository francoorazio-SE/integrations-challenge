#!/usr/bin/env bash
#
# connector/run.sh - the entry point the grader invokes.
#
# Contract (connector/README.md):
#
#   run.sh run --run-id ID
#              --erp-base-url URL --twin-base-url URL
#              --erp-export-dir PATH --twin-drop-dir PATH
#              --state-dir PATH --report-dir PATH
#              [--config FILE] [--no-chaos]
#   run.sh --version
#
# Exit codes: 0 clean, 2 completed with business exceptions (not a failure),
# 3 hard failure. Anything else fails the scenario.
#
# Credentials come from the environment, never from the command line:
# ERP_CLIENT_ID, ERP_CLIENT_SECRET, TWIN_CLIENT_ID, TWIN_CLIENT_SECRET,
# SOAP_USERNAME, SOAP_PASSWORD. Never print them, and never write them to a
# report or a state file.
#
# The subcommand and every flag are already the Go binary's own contract (see
# connector/main.go), so this script does not re-parse them: it locates the
# binary setup.sh built and execs it, forwarding argv and its exit code
# unchanged. `exec` (not a plain call) is what makes the exit code pass
# through exactly, without an intermediate shell exit translating it.
#
# VERSION is the connector's version, bumped here by hand whenever a change
# lands. It is not re-parsed or re-derived: it is exported once and read
# straight back by main.go's --version handler (see connectorVersion there),
# so run.sh stays the single place that names the version.

set -euo pipefail

VERSION="0.0.1"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN="$SCRIPT_DIR/bin/connector"

if [ ! -x "$BIN" ]; then
  echo "run.sh: $BIN not found or not executable; run setup.sh first" >&2
  exit 3
fi

export CONNECTOR_VERSION="$VERSION"
exec "$BIN" "$@"
