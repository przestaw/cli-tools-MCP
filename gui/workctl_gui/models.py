# Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
# This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

"""Plain data for the views: accounts and doctor reports parsed from workctl JSON, and the editable config file."""

from __future__ import annotations

import difflib
import json
import os
import re
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

# Supported profile kinds (internal/config: kinds, plus google which expands to api).
KINDS = ("github", "gitlab", "jira", "azure", "atlassian", "api", "google")

# Authentication schemes per kind; the first one is the default (internal/config: ValidateProfile).
AUTH_SCHEMES: dict[str, tuple[str, ...]] = {
    "github": ("bearer",),
    "gitlab": ("private-token", "bearer"),
    "jira": ("bearer", "basic"),
    "api": ("bearer", "basic", "header", "form", "oauth2", "none"),
    "google": ("oauth2",),
    "azure": ("",),
    "atlassian": ("",),
}

BACKENDS: dict[str, tuple[str, ...]] = {"github": ("native", "cli"), "gitlab": ("native", "cli")}
GOOGLE_SERVICES = ("drive", "cloud", "all")
DEFAULT_URLS = {"github": "https://api.github.com", "gitlab": "https://gitlab.com"}

# Kinds that `workctl KIND init --non-interactive` can create; the others are written to the config by the GUI.
INIT_KINDS = ("github", "gitlab", "jira")
REPO_KINDS = ("github", "gitlab", "azure")
PROJECT_KINDS = ("gitlab", "azure", "jira", "atlassian")


def split_list(text: str) -> list[str]:
    return [item.strip().strip("/") for item in re.split(r"[,\s]+", text) if item.strip().strip("/")]


@dataclass(frozen=True)
class Account:
    name: str
    kind: str
    host: str = ""
    url: str = ""
    backend: str = ""
    auth: str = ""
    username: str = ""
    default: bool = False
    read_only: bool = False
    default_repo: str = ""
    repos: tuple[str, ...] = ()
    default_project: str = ""
    projects: tuple[str, ...] = ()
    credential: str = ""

    @classmethod
    def from_json(cls, d: dict[str, Any]) -> Account:
        return cls(
            name=str(d.get("name", "")),
            kind=str(d.get("kind", "")),
            host=str(d.get("host", "")),
            url=str(d.get("url", "")),
            backend=str(d.get("backend", "")),
            auth=str(d.get("auth", "")),
            username=str(d.get("username", "")),
            default=bool(d.get("default", False)),
            read_only=bool(d.get("read_only", False)),
            default_repo=str(d.get("default_repo", "")),
            repos=tuple(d.get("repos") or ()),
            default_project=str(d.get("default_project", "")),
            projects=tuple(d.get("projects") or ()),
            credential=str(d.get("credential", "")),
        )

    def bindings(self) -> str:
        parts = []
        if self.repos or self.default_repo:
            parts.append("repos: " + ", ".join(self.repos or (self.default_repo,)))
        if self.projects or self.default_project:
            parts.append("projects: " + ", ".join(self.projects or (self.default_project,)))
        return "; ".join(parts)

    def credential_ok(self) -> bool:
        c = self.credential
        return c == "stored in keyring" or c.startswith(("set via environment", "logged in as"))

    def needs_login(self) -> bool:
        return self.backend == "cli" or self.auth == "oauth2"


def parse_accounts(data: dict[str, Any]) -> tuple[list[Account], bool]:
    """Profiles and policy.allow_write from `workctl accounts`."""
    return [Account.from_json(p) for p in data.get("profiles") or []], bool(data.get("allow_write", False))


@dataclass(frozen=True)
class DoctorCheck:
    name: str
    ok: bool
    code: str
    message: str
    hint: str


_HINTS = {
    "unauthorized": "The credential was rejected: store a new one with `workctl auth set-token --profile {name} --stdin`.",
    "forbidden": "The credential lacks a permission or scope for this call.",
    "dns": "The host name does not resolve: check the profile URL, VPN or DNS.",
    "timeout": "No answer in time: check network or VPN, or raise timeout_seconds.",
    "tls": "Certificate not trusted: set ca_file to the PEM bundle of your CA.",
    "refused": "Connection refused: wrong URL or port, or the service is down.",
}


def doctor_hint(name: str, code: str, message: str) -> str:
    if "keyring" in message.lower() and "dbus" in message.lower():
        return "The OS keyring (Secret Service) is not reachable: unlock or start it, or set the profile's token_env."
    command = re.search(r"`([^`]+)`", message)
    if command:
        return command.group(1)
    return _HINTS.get(code, "").format(name=name)


def parse_doctor(data: dict[str, Any]) -> list[DoctorCheck]:
    checks = []
    for name in sorted(data):
        entry = data[name] if isinstance(data[name], dict) else {}
        ok = bool(entry.get("ok", False))
        code = str(entry.get("code", ""))
        if ok:
            detail = entry.get("detail")
            message = detail if isinstance(detail, str) else json.dumps(detail, ensure_ascii=False) if detail else "ok"
        else:
            message = str(entry.get("error", ""))
        checks.append(DoctorCheck(name, ok, code, message, "" if ok else doctor_hint(name, code, message)))
    return checks


def build_profile(
    kind: str,
    *,
    url: str = "",
    auth: str = "",
    username: str = "",
    backend: str = "",
    repos: list[str] | None = None,
    projects: list[str] | None = None,
    service: str = "",
    client_id: str = "",
    auth_header: str = "",
) -> dict[str, Any]:
    """A new profile as written to config.json; empty values are left out."""
    p: dict[str, Any] = {"kind": kind}
    if url:
        p["url"] = url
    if kind == "google":
        p["service"] = service or "drive"
        p["oauth2"] = {"client_id": client_id}
        return p
    if auth:
        p["auth"] = auth
    if auth_header:
        p["auth_header"] = auth_header
    if auth == "oauth2" and client_id:
        p["oauth2"] = {"client_id": client_id}
    if username:
        p["username"] = username
    p["verify_tls"] = True
    if backend and backend != "native":
        p["backend"] = backend
    if repos:
        p["default_repo"], p["repos"] = repos[0], repos
    if projects:
        p["default_project"], p["projects"] = projects[0], projects
    return p


class ConfigConflict(Exception):
    """The config file changed on disk since it was loaded."""


@dataclass(frozen=True)
class NewProfile:
    """What the add-profile dialog collects; the credential is kept apart and never stored here."""

    name: str
    kind: str
    url: str = ""
    auth: str = ""
    username: str = ""
    backend: str = ""
    repos: tuple[str, ...] = ()
    projects: tuple[str, ...] = ()
    service: str = ""
    client_id: str = ""
    auth_header: str = ""
    make_default: bool = False

    def effective_kind(self) -> str:
        return "api" if self.kind == "google" else self.kind

    def config_entry(self) -> dict[str, Any]:
        return build_profile(
            self.kind, url=self.url, auth=self.auth, username=self.username, backend=self.backend,
            repos=list(self.repos), projects=list(self.projects), service=self.service,
            client_id=self.client_id, auth_header=self.auth_header,
        )

    def needs_login(self) -> bool:
        return self.kind == "google" or self.auth == "oauth2"

    def holds_token(self) -> bool:
        return self.backend != "cli" and self.auth != "none"


@dataclass
class ConfigDocument:
    """config.json as a JSON object; unknown fields are kept as they are."""

    path: Path
    original_text: str | None  # None: the file does not exist yet
    data: dict[str, Any] = field(default_factory=dict)

    @classmethod
    def load(cls, path: str | Path) -> ConfigDocument:
        p = Path(path)
        try:
            text = p.read_text(encoding="utf-8")
        except FileNotFoundError:
            return cls(p, None, {"version": 2, "defaults": {}, "profiles": {}, "policy": {"allow_write": False}})
        data = json.loads(text)
        if not isinstance(data, dict):
            raise TypeError(f"{p} is not a JSON object")
        return cls(p, text, data)

    def text(self) -> str:
        return json.dumps(self.data, indent=2, ensure_ascii=False) + "\n"

    def changed(self) -> bool:
        if self.original_text is None:
            return True
        return json.loads(self.original_text) != self.data

    def diff(self) -> str:
        before = (self.original_text or "").splitlines(keepends=True)
        after = self.text().splitlines(keepends=True)
        name = str(self.path)
        return "".join(difflib.unified_diff(before, after, fromfile=name, tofile=name + " (new)"))

    # Edits.

    def section(self, key: str) -> dict[str, Any]:
        value = self.data.get(key)
        if not isinstance(value, dict):
            value = {}
            self.data[key] = value
        return value

    def profiles(self) -> dict[str, Any]:
        return self.section("profiles")

    def add_profile(self, name: str, profile: dict[str, Any]) -> None:
        if not name.strip():
            raise ValueError("the profile name is empty")
        if name in self.profiles():
            raise ValueError(f"profile {name!r} already exists")
        self.profiles()[name] = profile

    def set_policy(self, key: str, value: bool) -> None:
        policy = self.section("policy")
        if value or key == "allow_write":
            policy[key] = value
        else:
            policy.pop(key, None)  # workctl leaves the confirm switches out when false

    def set_default(self, kind: str, name: str) -> None:
        defaults = self.section("defaults")
        if name:
            defaults[kind] = name
        else:
            defaults.pop(kind, None)

    def set_list(self, profile: str, key: str, values: list[str]) -> None:
        p = self.profiles()[profile]
        if values:
            p[key] = values
        else:
            p.pop(key, None)

    def set_text(self, profile: str, key: str, value: str) -> None:
        p = self.profiles()[profile]
        if value:
            p[key] = value
        else:
            p.pop(key, None)

    # Checks that need no workctl run; workctl itself validates when it loads the file.

    def problems(self) -> list[str]:
        out = []
        if self.data.get("version") != 2:
            out.append("version must be 2")
        policy = self.section("policy")
        if policy.get("confirm_writes") and policy.get("confirm_session_writes"):
            out.append("policy.confirm_writes and policy.confirm_session_writes cannot both be true")
        profiles = self.profiles()
        for kind, name in self.section("defaults").items():
            p = profiles.get(name)
            if not isinstance(p, dict):
                out.append(f"defaults.{kind} references unknown profile {name!r}")
            elif _effective_kind(p) != kind:
                out.append(f"defaults.{kind} references profile {name!r} of kind {_effective_kind(p)!r}")
        for name, p in profiles.items():
            if not isinstance(p, dict) or p.get("kind") not in KINDS:
                out.append(f"profiles.{name}.kind is unsupported")
        return out

    # Saving.

    def save(self) -> Path | None:
        """Writes atomically after copying the previous file to config.json.bak; returns the backup path."""
        current = self.path.read_text(encoding="utf-8") if self.path.exists() else None
        if current != self.original_text:
            raise ConfigConflict(f"{self.path} changed on disk since it was loaded; reload it first")
        self.path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
        backup = None
        if current is not None:
            backup = backup_path(self.path)
            _atomic_write(backup, self.path.read_bytes())
        text = self.text()
        _atomic_write(self.path, text.encode("utf-8"))
        self.original_text = text
        return backup


def _effective_kind(profile: dict[str, Any]) -> str:
    kind = str(profile.get("kind", ""))
    return "api" if kind == "google" else kind


def backup_path(path: str | Path) -> Path:
    p = Path(path)
    return p.with_name(p.name + ".bak")


def _atomic_write(path: Path, data: bytes) -> None:
    """Writes a sibling temporary file readable by the owner only, then renames it over path."""
    tmp = path.with_name(f".{path.name}.{os.getpid()}.tmp")
    fd = os.open(tmp, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    try:
        with os.fdopen(fd, "wb") as f:
            f.write(data)
            f.flush()
            os.fsync(f.fileno())
        os.replace(tmp, path)
    finally:
        if tmp.exists():
            tmp.unlink()


def restore_backup(path: str | Path) -> None:
    """Puts config.json.bak back in place of config.json."""
    p = Path(path)
    _atomic_write(p, backup_path(p).read_bytes())
