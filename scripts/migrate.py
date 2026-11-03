#!/usr/bin/env python3
# Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
# This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

"""Copy workctl keyring entries from the v2 account scheme to the v3+ scheme.

The v2 release stored a module's token under the account "<kind>:<url>"; v3 and later read
"<kind>:<profile>". For every profile in config.json this script copies the old entry to
the new account when the new one is missing. Old entries are kept unless --delete-old.
v1 is not migrated: it had a single service with a free-form token_account (default
"default") and no kind, so no mapping exists; store that token again with
`workctl auth set-token`. No secret is ever printed.

    python scripts/migrate.py --dry-run
    python scripts/migrate.py
    python scripts/migrate.py --delete-old

On Windows the Credential Manager is used directly (workctl stores entries under the
target "workctl:<account>"). Elsewhere the "keyring" package is required
(pip install keyring).
"""

import argparse
import json
import os
import sys
from pathlib import Path

SERVICE = "workctl"

if sys.platform == "win32":
    # go-keyring stores a generic credential with target "<service>:<account>" and a UTF-8 blob;
    # the Python keyring package uses other target names, so read and write through WinAPI instead.
    import ctypes
    import ctypes.wintypes as wt

    class _CREDENTIAL(ctypes.Structure):
        _fields_ = [
            ("Flags", wt.DWORD), ("Type", wt.DWORD), ("TargetName", wt.LPWSTR),
            ("Comment", wt.LPWSTR), ("LastWritten", wt.FILETIME),
            ("CredentialBlobSize", wt.DWORD), ("CredentialBlob", ctypes.POINTER(ctypes.c_byte)),
            ("Persist", wt.DWORD), ("AttributeCount", wt.DWORD), ("Attributes", ctypes.c_void_p),
            ("TargetAlias", wt.LPWSTR), ("UserName", wt.LPWSTR),
        ]

    _advapi = ctypes.WinDLL("advapi32", use_last_error=True)
    _GENERIC, _LOCAL_MACHINE = 1, 2

    def get_secret(account):
        cred = ctypes.POINTER(_CREDENTIAL)()
        if not _advapi.CredReadW(f"{SERVICE}:{account}", _GENERIC, 0, ctypes.byref(cred)):
            return None
        try:
            c = cred.contents
            return bytes(bytearray(c.CredentialBlob[: c.CredentialBlobSize]))
        finally:
            _advapi.CredFree(cred)

    def set_secret(account, value):
        blob = (ctypes.c_byte * len(value)).from_buffer_copy(value)
        cred = _CREDENTIAL(
            Type=_GENERIC, TargetName=f"{SERVICE}:{account}", CredentialBlobSize=len(value),
            CredentialBlob=blob, Persist=_LOCAL_MACHINE, UserName=account,
        )
        if not _advapi.CredWriteW(ctypes.byref(cred), 0):
            raise OSError(f"CredWrite failed: {ctypes.get_last_error()}")

    def delete_secret(account):
        _advapi.CredDeleteW(f"{SERVICE}:{account}", _GENERIC, 0)

else:
    try:
        import keyring
    except ImportError:
        print("the keyring package is missing: pip install keyring", file=sys.stderr)
        sys.exit(2)

    def get_secret(account):
        value = keyring.get_password(SERVICE, account)
        return None if value is None else value.encode()

    def set_secret(account, value):
        keyring.set_password(SERVICE, account, value.decode())

    def delete_secret(account):
        keyring.delete_password(SERVICE, account)


def config_path() -> Path:
    env = os.environ.get("WORKCTL_CONFIG")
    if env:
        return Path(env)
    if sys.platform == "win32" and os.environ.get("APPDATA"):
        return Path(os.environ["APPDATA"]) / "workctl" / "config.json"
    base = os.environ.get("XDG_CONFIG_HOME") or str(Path.home() / ".config")
    return Path(base) / "workctl" / "config.json"


def v2_accounts(kind: str, url: str):
    url = url.strip()
    yield f"{kind}:{url}"
    trimmed = url.rstrip("/")
    if trimmed != url:
        yield f"{kind}:{trimmed}"


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("--config", default=None, help="config.json (default: the workctl config path)")
    ap.add_argument("--dry-run", action="store_true", help="print what would change, write nothing")
    ap.add_argument("--overwrite", action="store_true", help="replace an existing v3 entry")
    ap.add_argument("--delete-old", action="store_true", help="delete the v2 entry after copying")
    args = ap.parse_args()

    path = Path(args.config) if args.config else config_path()
    try:
        cfg = json.loads(path.read_text(encoding="utf-8"))
    except FileNotFoundError:
        print(f"no config at {path}: nothing to migrate")
        return 0
    except OSError as err:
        print(f"cannot read {path}: {err}", file=sys.stderr)
        return 2

    changed = 0
    for name, profile in sorted(cfg.get("profiles", {}).items()):
        kind, url = profile.get("kind", ""), profile.get("url", "")
        if not kind or not url:
            continue
        new = f"{kind}:{name}"
        if get_secret(new) is not None and not args.overwrite:
            print(f"{new}: already stored, skipped")
            continue
        old = next((a for a in v2_accounts(kind, url) if get_secret(a) is not None), None)
        if old is None:
            print(f"{new}: no v2 entry for {url}; store the token with `workctl auth set-token --profile {name}`")
            continue
        if args.dry_run:
            print(f"{new}: would copy from {old}" + (" and delete the old entry" if args.delete_old else ""))
            continue
        set_secret(new, get_secret(old))
        if args.delete_old:
            delete_secret(old)
        print(f"{new}: copied from {old}" + (", old entry deleted" if args.delete_old else ""))
        changed += 1
    print(f"done: {changed} entr{'y' if changed == 1 else 'ies'} written" + (" (dry run)" if args.dry_run else ""))
    return 0


if __name__ == "__main__":
    sys.exit(main())
