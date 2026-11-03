#!/usr/bin/env sh
# Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
# This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.
# Cross-compile workctl into dist/ and write dist/SHA256SUMS. Publishing the files is a separate, manual step.
set -eu
cd "$(dirname "$0")/.."
go test ./...
rm -rf dist
mkdir dist
for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64; do
  os="${target%/*}"
  arch="${target#*/}"
  ext=""
  [ "$os" = windows ] && ext=".exe"
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath -ldflags '-s -w' -o "dist/workctl_${os}_${arch}${ext}" ./cmd/workctl
done
cd dist
if command -v sha256sum >/dev/null 2>&1; then sha256sum workctl_* > SHA256SUMS; else shasum -a 256 workctl_* > SHA256SUMS; fi
cat SHA256SUMS
