# Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
# This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

import json
import os
import stat
import sys
from collections.abc import Callable
from pathlib import Path

import pytest

GUI = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(GUI))
os.environ.setdefault("QT_QPA_PLATFORM", "offscreen")

from workctl_gui.cli import Workctl


def pytest_configure(config: pytest.Config) -> None:
    # Scratch files stay inside the worktree (gitignored), not in the system temporary directory.
    if not config.option.basetemp:
        config.option.basetemp = str(GUI / ".scratch" / "pytest")


@pytest.fixture
def fake(tmp_path: Path) -> Path:
    """An executable fake workctl; FAKE_WORKCTL_LOG and FAKE_WORKCTL_CONFIG point into tmp_path."""
    script = tmp_path / "workctl"
    source = Path(__file__).parent / "fake_workctl.py"
    # A shell wrapper, because a shebang cannot hold an interpreter path with spaces.
    script.write_text(f'#!/bin/sh\nexec "{sys.executable}" "{source}" "$@"\n', encoding="utf-8")
    script.chmod(script.stat().st_mode | stat.S_IXUSR)
    return script


@pytest.fixture
def workctl(fake: Path, tmp_path: Path) -> Workctl:
    config = tmp_path / "config.json"
    config.write_text(json.dumps({"version": 2, "profiles": {}, "policy": {"allow_write": False}}) + "\n")
    env = dict(os.environ, FAKE_WORKCTL_LOG=str(tmp_path / "calls.jsonl"), FAKE_WORKCTL_CONFIG=str(config))
    return Workctl(str(fake), env=env, timeout=10)


@pytest.fixture
def calls(tmp_path: Path) -> Callable[[], list[dict]]:
    def read() -> list[dict]:
        log = tmp_path / "calls.jsonl"
        return [json.loads(line) for line in log.read_text().splitlines()] if log.exists() else []
    return read
