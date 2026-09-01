#!/usr/bin/env sh
set -eu
cd "$(dirname "$0")/.."
go test ./...
go build -trimpath -ldflags '-s -w' -o workctl .
