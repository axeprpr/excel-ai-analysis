#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
go test "$ROOT_DIR/cmd/server" -run TestRootAndHealthRoutes -count=1
echo "readyz ok"
echo "smoke ok"
