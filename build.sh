#!/usr/bin/env sh
set -eu
cd "$(dirname "$0")"
go test ./...
go vet ./...
mkdir -p dist
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o dist/OpenDownload-windows-amd64.exe .
