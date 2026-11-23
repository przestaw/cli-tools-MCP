# Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
# This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

"""Multi-step operations of the GUI, without Qt: each step is a workctl command or a config file edit."""

from __future__ import annotations

from collections.abc import Callable
from pathlib import Path

from .cli import Result, Workctl, WorkctlError
from .models import INIT_KINDS, ConfigDocument, NewProfile, restore_backup

LineSink = Callable[[str], None]


def log_result(log: LineSink, result: Result) -> None:
    log("$ workctl " + " ".join(result.command))
    for line in result.output().splitlines():
        log("  " + line)
    if not result.ok:
        log(f"  (exit code {result.returncode})")


def save_and_check(wc: Workctl, doc: ConfigDocument, log: LineSink) -> None:
    """Saves the config with a backup, then lets workctl load it; a file workctl rejects is rolled back."""
    problems = doc.problems()
    if problems:
        raise WorkctlError("config not saved: " + "; ".join(problems))
    existed = doc.original_text is not None
    backup = doc.save()
    log(f"wrote {doc.path}" + (f" (previous version in {backup})" if backup else ""))
    check = wc.config_show()
    log("$ workctl " + " ".join(check.command) + ": " + ("ok, workctl loads the file" if check.ok else check.output()))
    if not check.ok:
        if existed:
            restore_backup(doc.path)
        else:
            Path(doc.path).unlink(missing_ok=True)
        raise WorkctlError("workctl cannot load the new config, so it was rolled back: " + check.stderr.strip())


def create_profile(wc: Workctl, spec: NewProfile, secret: str, log: LineSink, start_login: bool) -> None:
    """Adds a profile and stores its credential: `KIND init` where it exists, else a direct config edit."""
    doc = ConfigDocument.load(wc.config_path())
    if spec.name in doc.profiles():
        raise WorkctlError(f"profile {spec.name!r} already exists")

    if spec.kind in INIT_KINDS:
        bound = list(spec.repos) if spec.kind == "github" else list(spec.projects) if spec.kind == "gitlab" else []
        result = wc.endpoint_init(
            spec.kind, spec.name, url=spec.url, auth=spec.auth, username=spec.username, backend=spec.backend,
            projects=bound, default_project=bound[0] if bound else "",
        )
        log_result(log, result)
        if not result.ok:
            raise WorkctlError(f"profile {spec.name!r} was not created")
        _add_what_init_cannot(wc, spec, log)
    else:
        log(f"no workctl command adds {spec.kind} profiles: writing the config file directly")
        doc.add_profile(spec.name, spec.config_entry())
        if spec.make_default or not doc.section("defaults").get(spec.effective_kind()):
            doc.set_default(spec.effective_kind(), spec.name)
        save_and_check(wc, doc, log)

    if secret and spec.holds_token():
        result = wc.set_token(spec.name, secret)
        log_result(log, result)
        if not result.ok:
            raise WorkctlError(f"profile {spec.name!r} was created, but its credential was not stored")
    if start_login and spec.needs_login():
        log(f"$ workctl auth login --profile {spec.name}")
        code = wc.login(spec.name, lambda line: log("  " + line))
        if code != 0:
            raise WorkctlError(f"login failed (exit code {code}); retry with Log in on the Profiles tab")


def _add_what_init_cannot(wc: Workctl, spec: NewProfile, log: LineSink) -> None:
    """`init` binds github repos and gitlab projects only, and makes only the first profile of a kind the default."""
    doc = ConfigDocument.load(wc.config_path())
    if spec.kind == "jira" and spec.projects:
        doc.set_list(spec.name, "projects", list(spec.projects))
        doc.set_text(spec.name, "default_project", spec.projects[0])
    if spec.kind == "gitlab" and spec.repos:
        doc.set_list(spec.name, "repos", list(spec.repos))
    if spec.make_default:
        doc.set_default(spec.kind, spec.name)
    if doc.changed():
        log("init has no flag for the remaining fields: writing them to the config file directly")
        save_and_check(wc, doc, log)
