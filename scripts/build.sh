#!/usr/bin/env sh
# Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
# This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.
set -eu
cd "$(dirname "$0")/.."
go test ./...
go build -trimpath -ldflags '-s -w' -o workctl ./cmd/workctl
