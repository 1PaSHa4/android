#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
# Resolve a pinned upstream release commit to its Go-compatible version.
go get github.com/xtls/xray-core@676eaf66f8f05d4d558ee7a74b4736623d3feeb9
go mod tidy
go vet ./...
go test -race -count=1 ./...
