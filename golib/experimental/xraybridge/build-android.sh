#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
go get github.com/xtls/xray-core@676eaf66f8f05d4d558ee7a74b4736623d3feeb9
go get golang.org/x/mobile@v0.0.0-20261011034528-197770444867
go mod tidy
GOBIN="$PWD/.bin" go install golang.org/x/mobile/cmd/gomobile@v0.0.0-20261011034528-197770444867
GOBIN="$PWD/.bin" go install golang.org/x/mobile/cmd/gobind@v0.0.0-20261011034528-197770444867
export PATH="$PWD/.bin:$PATH"
mkdir -p out
gomobile bind -v -trimpath -ldflags="-s -w" -target=android/arm64 -androidapi 26 -o out/vpnmobile.aar ./vpnmobile
