# Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
# This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

"""A stand-in for the workctl binary in tests (run through a wrapper script): canned output, and a log of argv and stdin per call."""

import json
import os
import sys
import time

ACCOUNTS = {
    "profiles": [
        {"name": "gh", "kind": "github", "host": "github.com", "url": "https://api.github.com", "backend": "native",
         "auth": "bearer", "default": True, "repos": ["o/r"], "credential": "stored in keyring"},
        {"name": "jira", "kind": "jira", "host": "jira.example.com", "url": "https://jira.example.com",
         "backend": "native", "auth": "bearer", "projects": ["ABC"], "credential": "missing"},
    ],
    "defaults": {"github": "gh"},
    "allow_write": False,
}

DOCTOR = {
    "gh": {"ok": True, "detail": {"kind": "github", "login": "octo"}},
    "jira": {"ok": False, "code": "other",
             "error": 'no credential for profile "jira": run `workctl auth set-token --profile jira --stdin`'},
    "skills": {"ok": True, "detail": {"current": None}},
}


def main() -> int:
    args = sys.argv[1:]
    if args[:1] == ["--config"]:
        args = args[2:]
    stdin = "" if sys.stdin is None or sys.stdin.isatty() else sys.stdin.read()
    with open(os.environ["FAKE_WORKCTL_LOG"], "a", encoding="utf-8") as log:
        log.write(json.dumps({"argv": sys.argv[1:], "stdin": stdin}) + "\n")
    config = os.environ.get("FAKE_WORKCTL_CONFIG", "")

    match args:
        case ["version"]:
            print("0.4.8")
        case ["config", "path"]:
            print(config)
        case ["config", "show"]:
            try:
                with open(config, encoding="utf-8") as f:
                    data = json.load(f)
            except (OSError, ValueError) as e:
                print(f"error: {e}", file=sys.stderr)
                return 1
            if data.get("version") != 2:
                print("error: config is not version 2", file=sys.stderr)
                return 1
            print(json.dumps(data))
        case ["accounts"]:
            print(json.dumps(ACCOUNTS, indent=2))
        case ["doctor"]:
            print(json.dumps(DOCTOR, indent=2))
            print("error: one or more connectivity checks failed", file=sys.stderr)
            return 1
        case ["auth", "set-token", "--profile", name, "--stdin"]:
            if not stdin.strip():
                print("error: token cannot be empty", file=sys.stderr)
                return 1
            print(f"token stored in keyring for profile {name}")
        case ["auth", "login", "--profile", name, *_]:
            print(f"Open this URL to authorize profile {name}:")
            print("https://auth.example.com/authorize?x=1")
        case [kind, "init", *rest] if kind in ("github", "gitlab", "jira"):
            flags = dict(zip(rest[1::2], rest[2::2])) if rest[:1] == ["--non-interactive"] else {}
            with open(config, encoding="utf-8") as f:
                data = json.load(f)
            name = flags["--profile"]
            data.setdefault("profiles", {})[name] = {"kind": kind, "url": flags.get("--url", ""), "verify_tls": True}
            data.setdefault("defaults", {}).setdefault(kind, name)
            with open(config, "w", encoding="utf-8") as f:
                json.dump(data, f, indent=2)
            print(f"saved: {config}")
        case ["sleep"]:
            time.sleep(10)
        case ["garbage"]:
            print("not json")
        case _:
            print(f"error: unknown command {args!r}", file=sys.stderr)
            return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
