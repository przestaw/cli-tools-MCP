#!/usr/bin/env python3
# Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
# This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

"""Build workctl and register it with the agents on this machine (Windows, macOS, Linux).

    python3 scripts/install.py                  build into ~/.local/bin, migrate keyring entries
    python3 scripts/install.py --all            and register with every agent found below
    python3 scripts/install.py --claude --gitkraken

  --vscode     MCP server in the user mcp.json of VS Code, Insiders and VSCodium; skill in ~/.copilot/skills
  --claude     MCP server in Claude Code (user scope); skill in ~/.claude/skills
  --codex      MCP server in Codex; skill in $CODEX_HOME/skills (default ~/.codex/skills)
  --gitkraken  GitKraken MCP (bundled with GitLens) for the Claude Code and Codex found here,
               read-only while workctl writes are off. VS Code gets it from GitLens itself.

The skills are embedded in the binary and copied by `workctl skills install` (nothing else in the skills
directories is touched). Check them later with `workctl skills status` or `workctl doctor`.
  --with-compact-output   also install the optional compact-output skill
  --skills-only           only refresh the skills of the selected agents (uses the workctl found on PATH or
                          in the install directory; builds only when there is none)

A server already registered as "workctl" is left as it is (with a warning when it runs another binary).
Do not combine this with the Claude Code plugin (two skills named workctl). Building needs Go. The claude, codex and gk
CLIs are taken from PATH or from the VS Code extensions.
"""

import argparse
import json
import os
import re
import shutil
import subprocess
import sys
import time
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
WIN = sys.platform == "win32"
EXE = ".exe" if WIN else ""
HOME = Path.home()
EXTENSIONS = [HOME / ".vscode/extensions", HOME / ".vscode-insiders/extensions", HOME / ".vscode-oss/extensions"]


def run(*cmd, quiet=False, cwd=None):
    return subprocess.run([str(c) for c in cmd], cwd=cwd, text=True, capture_output=quiet)


def must(*cmd, cwd=None):
    if run(*cmd, cwd=cwd).returncode != 0:
        sys.exit("failed: " + " ".join(str(c) for c in cmd))


def build(dest):
    dest.mkdir(parents=True, exist_ok=True)
    binary = dest / ("workctl" + EXE)
    moved = None
    if WIN and binary.exists():
        # A running MCP server locks the file, but Windows still allows renaming it.
        for old in dest.glob("workctl.exe.old-*"):
            try:
                old.unlink()
            except OSError:
                pass
        moved = binary.rename(dest / f"workctl.exe.old-{int(time.time())}")
    if run("go", "build", "-trimpath", "-ldflags", "-s -w", "-o", binary, "./cmd/workctl", cwd=ROOT).returncode:
        if moved:
            moved.rename(binary)
        sys.exit("go build failed")
    print(f"installed: {binary}")
    return binary


def ensure_path(dest):
    if not WIN:
        if str(dest) not in os.environ.get("PATH", "").split(os.pathsep):
            print(f'add to PATH: export PATH="{dest}:$PATH"')
        return
    import ctypes
    import winreg
    with winreg.OpenKey(winreg.HKEY_CURRENT_USER, "Environment", 0, winreg.KEY_READ | winreg.KEY_WRITE) as key:
        try:
            value, kind = winreg.QueryValueEx(key, "Path")
        except FileNotFoundError:
            value, kind = "", winreg.REG_EXPAND_SZ
        if str(dest).lower() in (p.lower() for p in value.split(";")):
            return
        winreg.SetValueEx(key, "Path", 0, kind, f"{value};{dest}" if value else str(dest))
    ctypes.windll.user32.SendMessageTimeoutW(0xFFFF, 0x1A, 0, "Environment", 2, 5000, None)
    print(f"added {dest} to the user PATH (open a new terminal)")


def install_skills(binary, agent, with_compact):
    extra = ["--with", "compact-output"] if with_compact else []
    if run(binary, "skills", "install", "--target", agent, *extra).returncode:
        print(f"skills for {agent} not installed; run: {binary} skills install --target {agent}")


def warn_other_binary(where, command, binary, fix):
    """One line when the registered server runs another binary than the one just built; the config stays as it is."""
    if not command:
        return
    if str(command) == "workctl":
        command = shutil.which("workctl") or command
    if os.path.normcase(os.path.abspath(str(command))) != os.path.normcase(os.path.abspath(str(binary))):
        print(f"warning: {where} runs {command}, not {binary}; to switch: {fix}")


def vscode_user_dirs():
    if WIN:
        base = Path(os.environ.get("APPDATA", HOME / "AppData/Roaming"))
    elif sys.platform == "darwin":
        base = HOME / "Library/Application Support"
    else:
        base = Path(os.environ.get("XDG_CONFIG_HOME", HOME / ".config"))
    return [base / p / "User" for p in ("Code", "Code - Insiders", "VSCodium") if (base / p / "User").is_dir()]


def find_cli(name, pattern):
    """The CLI on PATH, else the newest copy bundled with a VS Code extension."""
    found = shutil.which(name)
    if found:
        return found
    hits = [p for d in EXTENSIONS for p in d.glob(pattern) if p.is_file()]
    return str(max(hits, key=lambda p: p.stat().st_mtime)) if hits else None


def find_gk():
    found = shutil.which("gk")
    if found:
        return found
    hits = [d / "globalStorage/eamodio.gitlens" / ("gk" + EXE) for d in vscode_user_dirs()]
    return next((str(p) for p in hits if p.is_file()), None)


def claude_cli():
    return find_cli("claude", f"anthropic.claude-code-*/resources/native-binary/claude{EXE}")


def codex_cli():
    return find_cli("codex", f"openai.chatgpt-*/bin/*/codex{EXE}")


def register_vscode(binary):
    server = {"type": "stdio", "command": str(binary), "args": ["mcp"]}
    for user in vscode_user_dirs():
        # Profiles with their own MCP configuration have their own mcp.json; the others share the user one.
        for path in [user / "mcp.json", *sorted(user.glob("profiles/*/mcp.json"))]:
            data = {}
            if path.exists():
                try:
                    data = json.loads(path.read_text(encoding="utf-8") or "{}")
                except ValueError:
                    print(f'{path}: not plain JSON, add by hand: "workctl": {json.dumps(server)}')
                    continue
            servers = data.setdefault("servers", {})
            if "workctl" in servers:
                print(f"{path}: workctl already registered, left as it is")
                warn_other_binary(path, servers["workctl"].get("command"), binary, f"set its command to {binary}")
                continue
            servers["workctl"] = server
            path.write_text(json.dumps(data, indent=2) + "\n", encoding="utf-8")
            print(f"{path}: workctl added")


def register_claude(cli, binary):
    current = run(cli, "mcp", "get", "workctl", quiet=True)
    if current.returncode == 0:
        print("Claude Code already has an MCP server named workctl: left as it is")
        found = re.search(r"^\s*Command:\s*(.+?)\s*$", current.stdout or "", re.M)
        fix = f"claude mcp remove workctl -s user && claude mcp add --scope user workctl -- {binary} mcp"
        warn_other_binary("Claude Code", found and found.group(1), binary, fix)
    else:
        must(cli, "mcp", "add", "--scope", "user", "workctl", "--", binary, "mcp")


def register_codex(cli, binary):
    if re.search(r"^\s*workctl\s", run(cli, "mcp", "list", quiet=True).stdout or "", re.M):
        print("Codex already lists an MCP server named workctl: left as it is")
        try:
            command = json.loads(run(cli, "mcp", "get", "workctl", "--json", quiet=True).stdout)["transport"]["command"]
        except (ValueError, KeyError, TypeError):
            command = None
        warn_other_binary("Codex", command, binary, f"codex mcp remove workctl && codex mcp add workctl -- {binary} mcp")
    else:
        must(cli, "mcp", "add", "workctl", "--", binary, "mcp")


def register_gitkraken(gk, binary, clients):
    if not clients:
        print("GitKraken: no Claude Code or Codex to register it with")
        return
    config = run(binary, "config", "path", quiet=True).stdout.strip()
    try:
        writes = json.loads(Path(config).read_text(encoding="utf-8")).get("policy", {}).get("allow_write", False)
    except (OSError, ValueError):
        writes = False
    must(gk, "mcp", "install", *clients, f"--readonly={'false' if writes else 'true'}")
    print(f"GitKraken MCP registered for {', '.join(clients)}" + ("" if writes else " (read-only, as workctl writes are off)"))


def main():
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    for agent in ("vscode", "claude", "codex", "gitkraken"):
        ap.add_argument(f"--{agent}", action="store_true")
    ap.add_argument("--all", action="store_true", help="every agent found on this machine")
    ap.add_argument("--with-compact-output", action="store_true", help="also install the compact-output skill")
    ap.add_argument("--skills-only", action="store_true", help="only refresh the skills of the selected agents")
    args = ap.parse_args()

    dest = Path(os.environ.get("WORKCTL_INSTALL_DIR", HOME / ".local/bin"))
    binary = shutil.which("workctl") or (dest / ("workctl" + EXE) if (dest / ("workctl" + EXE)).is_file() else None)
    if not (args.skills_only and binary):
        binary = build(dest)
    if not args.skills_only:
        if run(sys.executable, ROOT / "scripts/migrate.py").returncode:
            print("keyring migration skipped; run scripts/migrate.py later")
        ensure_path(dest)

    claude, codex, gk = claude_cli(), codex_cli(), find_gk()
    agents = {  # name -> (found on this machine, MCP registration, message when missing)
        "vscode": (bool(vscode_user_dirs()), lambda: register_vscode(binary), None),
        "claude": (bool(claude), lambda: register_claude(claude, binary), "Claude Code not found: skipped"),
        "codex": (bool(codex), lambda: register_codex(codex, binary), "Codex not found: skipped"),
    }
    for agent, (found, register, missing) in agents.items():
        if not (getattr(args, agent) or args.all):
            continue
        if not found:
            if missing:
                print(missing)
            continue
        if not args.skills_only:
            register()
        install_skills(binary, agent, args.with_compact_output)
    if (args.gitkraken or args.all) and not args.skills_only:
        if not gk:
            print("GitKraken CLI not found (install GitLens): skipped")
        else:
            # gk registers itself through the agents' own CLIs, which may live only inside the extensions.
            os.environ["PATH"] = os.pathsep.join([str(Path(c).parent) for c in (claude, codex) if c] + [os.environ["PATH"]])
            register_gitkraken(gk, binary, [n for n, c in (("claude-cli", claude), ("codex", codex)) if c])
    print("restart running workctl MCP servers (VS Code: MCP: List Servers > workctl > Restart)")


if __name__ == "__main__":
    main()
