#!/usr/bin/env sh
# Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
# This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.
# Build workctl into $WORKCTL_INSTALL_DIR (default ~/.local/bin); register it with the agents named on the command line:
#   --vscode  MCP server in the VS Code user profile
#   --claude  MCP server in Claude Code (user scope) and the skill in ~/.claude/skills (VS Code reads it there too)
#   --codex   MCP server in Codex
# A server that is already registered under the name "workctl" is left as it is.
set -eu
dest="${WORKCTL_INSTALL_DIR:-$HOME/.local/bin}"
cd "$(dirname "$0")/.."
mkdir -p "$dest"
go build -trimpath -ldflags '-s -w' -o "$dest/workctl" ./cmd/workctl
echo "installed: $dest/workctl"
if command -v python3 >/dev/null 2>&1; then
  # Copies v2 keyring entries (<kind>:<url>) to the v3 accounts (<kind>:<profile>); no-op when none exist.
  python3 scripts/migrate.py || echo "keyring migration failed; run scripts/migrate.py later" >&2
else
  echo "python3 not found: if tokens were stored by the v2 release, run scripts/migrate.py later"
fi
case ":$PATH:" in
  *":$dest:"*) ;;
  *) echo "add to PATH: export PATH=\"$dest:\$PATH\"" ;;
esac
for arg in "$@"; do
  case "$arg" in
    --vscode)
      code --add-mcp "{\"name\":\"workctl\",\"type\":\"stdio\",\"command\":\"$dest/workctl\",\"args\":[\"mcp\"]}"
      echo "workctl MCP server added to the VS Code user profile" ;;
    --claude)
      if claude mcp get workctl >/dev/null 2>&1; then
        echo "Claude Code already has an MCP server named workctl: left as it is"
      else
        claude mcp add --scope user workctl -- "$dest/workctl" mcp
      fi
      mkdir -p "$HOME/.claude/skills/workctl"
      cp plugins/workctl/skills/workctl/SKILL.md "$HOME/.claude/skills/workctl/SKILL.md"
      echo "workctl skill installed in ~/.claude/skills/workctl" ;;
    --codex)
      if codex mcp list 2>/dev/null | grep -Eq '^[[:space:]]*workctl([[:space:]]|$)'; then
        echo "Codex already lists a server named workctl: left as it is"
      else
        codex mcp add workctl -- "$dest/workctl" mcp
      fi ;;
    *) echo "unknown option $arg (use --vscode, --claude, --codex)" >&2; exit 2 ;;
  esac
done
