#!/usr/bin/env sh
set -eu
dest="${WORKCTL_INSTALL_DIR:-$HOME/.local/bin}"
cd "$(dirname "$0")/.."
mkdir -p "$dest"
go build -trimpath -ldflags '-s -w' -o "$dest/workctl" .
echo "installed: $dest/workctl"
case ":$PATH:" in
  *":$dest:"*) ;;
  *) echo "add to PATH: export PATH=\"$dest:\$PATH\"" ;;
esac
