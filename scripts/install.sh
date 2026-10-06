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
if [ "${1:-}" = "--vscode" ]; then
  code --add-mcp "{\"name\":\"workctl\",\"type\":\"stdio\",\"command\":\"$dest/workctl\",\"args\":[\"mcp\"]}"
  echo "workctl MCP server added to the VS Code user profile"
fi
