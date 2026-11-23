# Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
# This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

import json
import os
import stat
from pathlib import Path

import pytest

from workctl_gui import actions
from workctl_gui.cli import Workctl, WorkctlError
from workctl_gui.models import ConfigConflict, ConfigDocument, NewProfile, parse_accounts, parse_doctor

SECRET = "ghp_TOPSECRET123"


def test_run_captures_output_and_passes_config(workctl: Workctl, calls) -> None:
    workctl.config = "/x/config.json"
    assert workctl.version() == "0.4.8"
    assert calls()[-1]["argv"] == ["--config", "/x/config.json", "version"]
    failed = workctl.run(["nope"])
    assert failed.returncode == 1 and "unknown command" in failed.stderr


def test_set_token_sends_the_secret_on_stdin_only(workctl: Workctl, calls) -> None:
    result = workctl.set_token("gh", f"  {SECRET}  ")
    assert result.ok and SECRET not in result.output()
    call = calls()[-1]
    assert call["argv"] == ["auth", "set-token", "--profile", "gh", "--stdin"]
    assert call["stdin"] == SECRET + "\n"
    assert SECRET not in " ".join(result.command)
    assert all(SECRET not in value for value in (workctl.env or {}).values())


@pytest.mark.parametrize("secret", ["", "   ", "a\nb"])
def test_set_token_rejects_empty_or_multiline_without_running(workctl: Workctl, calls, secret: str) -> None:
    with pytest.raises(WorkctlError):
        workctl.set_token("gh", secret)
    assert calls() == []


def test_doctor_parses_json_despite_exit_code_1(workctl: Workctl) -> None:
    data, result = workctl.doctor()
    assert result.returncode == 1
    checks = {c.name: c for c in parse_doctor(data)}
    assert checks["gh"].ok and "octo" in checks["gh"].message
    assert not checks["jira"].ok
    assert checks["jira"].hint == "workctl auth set-token --profile jira --stdin"


def test_accounts_are_parsed(workctl: Workctl) -> None:
    accounts, allow_write = parse_accounts(workctl.accounts())
    assert [a.name for a in accounts] == ["gh", "jira"] and not allow_write
    assert accounts[0].credential_ok() and not accounts[1].credential_ok()
    assert accounts[0].bindings() == "repos: o/r"


def test_errors_for_bad_json_missing_binary_and_timeout(workctl: Workctl) -> None:
    with pytest.raises(WorkctlError, match="did not print JSON"):
        workctl.run_json(["garbage"])
    with pytest.raises(WorkctlError, match="failed"):
        workctl.run_json(["nope"])
    with pytest.raises(WorkctlError, match="timed out") as e:
        workctl.run(["sleep"], stdin=SECRET, timeout=0.5)
    assert SECRET not in str(e.value)
    with pytest.raises(WorkctlError, match="not found"):
        Workctl("/nonexistent/workctl").version()


def test_login_streams_lines(workctl: Workctl) -> None:
    lines: list[str] = []
    assert workctl.login("gd", lines.append) == 0
    assert lines[0] == "Open this URL to authorize profile gd:"


def test_config_save_keeps_unknown_fields_and_backs_up(tmp_path: Path) -> None:
    path = tmp_path / "config.json"
    path.write_text(json.dumps({"version": 2, "future": {"x": 1}, "policy": {"allow_write": False}}))
    doc = ConfigDocument.load(path)
    doc.set_policy("allow_write", True)
    assert "+    \"allow_write\": true" in doc.diff()
    backup = doc.save()
    assert backup is not None and json.loads(backup.read_text())["policy"]["allow_write"] is False
    saved = json.loads(path.read_text())
    assert saved["future"] == {"x": 1} and saved["policy"]["allow_write"] is True
    assert stat.S_IMODE(os.stat(path).st_mode) == 0o600
    path.write_text("{}")
    with pytest.raises(ConfigConflict):
        doc.save()


def test_create_api_profile_writes_config_and_stores_token(workctl: Workctl, calls) -> None:
    log: list[str] = []
    spec = NewProfile(name="art", kind="api", url="https://art.example.com", auth="bearer")
    actions.create_profile(workctl, spec, SECRET, log.append, start_login=False)
    doc = ConfigDocument.load(workctl.config_path())
    assert doc.profiles()["art"] == {"kind": "api", "url": "https://art.example.com", "auth": "bearer", "verify_tls": True}
    assert doc.data["defaults"] == {"api": "art"}
    token_call = [c for c in calls() if c["argv"][:2] == ["auth", "set-token"]]
    assert token_call[0]["stdin"] == SECRET + "\n"
    assert all(SECRET not in line for line in log)
    assert all(SECRET not in " ".join(c["argv"]) for c in calls())


def test_create_jira_profile_uses_init_then_adds_projects(workctl: Workctl, calls) -> None:
    spec = NewProfile(name="j", kind="jira", url="https://jira.example.com", projects=("ABC",))
    actions.create_profile(workctl, spec, "", lambda _: None, start_login=False)
    assert calls()[1]["argv"][:3] == ["jira", "init", "--non-interactive"]
    profile = ConfigDocument.load(workctl.config_path()).profiles()["j"]
    assert profile["projects"] == ["ABC"] and profile["default_project"] == "ABC"
