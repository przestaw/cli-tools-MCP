#!/usr/bin/env sh
# Build workctl into $WORKCTL_INSTALL_DIR (default ~/.local/bin); register it with the agents named on the command line:
#   --vscode  MCP server in the VS Code user profile
#   --claude  MCP server in Claude Code (user scope) and the skill in ~/.claude/skills (VS Code reads it there too)
#   --codex   MCP server in Codex
# A server that is already registered under the name "workctl" is left as it is.
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
