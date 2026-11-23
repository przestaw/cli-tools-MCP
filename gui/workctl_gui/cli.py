# Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
# This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

"""The only module that runs processes: a thin wrapper around the workctl binary.

Secrets are passed on stdin only, never in argv or the environment, and never appear in
results, exceptions or logs produced here.
"""

from __future__ import annotations

import json
import os
import shutil
import subprocess
import threading
from collections.abc import Callable, Sequence
from dataclasses import dataclass
from typing import Any

DEFAULT_TIMEOUT = 60.0
LOGIN_TIMEOUT = 330.0  # workctl gives the OAuth loopback login 5 minutes


class WorkctlError(Exception):
    """workctl could not be run, timed out, failed, or printed something that is not the expected JSON."""

    def __init__(self, message: str, result: Result | None = None) -> None:
        super().__init__(message)
        self.result = result


@dataclass(frozen=True)
class Result:
    command: tuple[str, ...]  # argv without the binary path; never contains a secret
    returncode: int
    stdout: str
    stderr: str

    @property
    def ok(self) -> bool:
        return self.returncode == 0

    def output(self) -> str:
        """stdout and stderr together, for display."""
        return "\n".join(part.rstrip("\n") for part in (self.stdout, self.stderr) if part.strip())


def default_binary() -> str:
    return os.environ.get("WORKCTL_GUI_BINARY") or shutil.which("workctl") or "workctl"


class Workctl:
    """Runs `workctl [--config FILE] ARGS...` with a timeout and captured output."""

    def __init__(
        self,
        binary: str | None = None,
        config: str | None = None,
        timeout: float = DEFAULT_TIMEOUT,
        env: dict[str, str] | None = None,
    ) -> None:
        self.binary = binary or default_binary()
        self.config = config or None
        self.timeout = timeout
        self.env = env  # None inherits the environment of the GUI

    def _args(self, args: Sequence[str]) -> list[str]:
        prefix = ["--config", self.config] if self.config else []
        return prefix + list(args)

    def run(self, args: Sequence[str], *, stdin: str | None = None, timeout: float | None = None) -> Result:
        full = self._args(args)
        try:
            proc = subprocess.run(
                [self.binary, *full],
                input=stdin,
                stdin=None if stdin is not None else subprocess.DEVNULL,
                capture_output=True,
                text=True,
                encoding="utf-8",
                errors="replace",
                timeout=timeout or self.timeout,
                env=self.env,
                check=False,
            )
        except FileNotFoundError:
            raise WorkctlError(f"workctl binary not found: {self.binary}") from None
        except PermissionError:
            raise WorkctlError(f"workctl binary is not executable: {self.binary}") from None
        except subprocess.TimeoutExpired as e:
            # The exception carries the stdin input; drop it so a token cannot leak through a traceback.
            raise WorkctlError(f"`workctl {' '.join(full)}` timed out after {e.timeout:.0f}s") from None
        return Result(tuple(full), proc.returncode, proc.stdout, proc.stderr)

    def run_json(self, args: Sequence[str], *, allow_failure: bool = False) -> tuple[Any, Result]:
        """Runs a command that prints JSON. allow_failure accepts a non-zero exit when stdout is still JSON."""
        result = self.run(args)
        if not result.ok and not allow_failure:
            raise WorkctlError(_failure(result), result)
        try:
            return json.loads(result.stdout), result
        except json.JSONDecodeError:
            if not result.ok:
                raise WorkctlError(_failure(result), result) from None
            raise WorkctlError(f"`workctl {' '.join(result.command)}` did not print JSON", result) from None

    def stream(self, args: Sequence[str], on_line: Callable[[str], None], *, timeout: float = LOGIN_TIMEOUT) -> int:
        """Runs a long command (a login), passing each output line to on_line; returns the exit code."""
        full = self._args(args)
        try:
            proc = subprocess.Popen(
                [self.binary, *full],
                stdin=subprocess.DEVNULL,
                stdout=subprocess.PIPE,
                stderr=subprocess.STDOUT,
                text=True,
                encoding="utf-8",
                errors="replace",
                env=self.env,
            )
        except OSError as e:
            raise WorkctlError(f"cannot run workctl binary {self.binary}: {e.strerror}") from None
        timer = threading.Timer(timeout, proc.kill)
        timer.start()
        try:
            assert proc.stdout is not None
            for line in proc.stdout:
                on_line(line.rstrip("\n"))
            code = proc.wait()
        finally:
            timer.cancel()
        if code < 0:
            on_line(f"(stopped after {timeout:.0f}s)")
        return code

    # Commands used by the GUI.

    def version(self) -> str:
        result = self.run(["version"])
        if not result.ok:
            raise WorkctlError(_failure(result), result)
        return result.stdout.strip()

    def config_path(self) -> str:
        result = self.run(["config", "path"])
        if not result.ok:
            raise WorkctlError(_failure(result), result)
        return result.stdout.strip()

    def config_show(self) -> Result:
        """Loads and parses the config file (read-only); a failure means workctl cannot load it."""
        return self.run(["config", "show"])

    def accounts(self) -> dict[str, Any]:
        data, _ = self.run_json(["accounts"])
        if not isinstance(data, dict):
            raise WorkctlError("`workctl accounts` did not print a JSON object")
        return data

    def doctor(self) -> tuple[dict[str, Any], Result]:
        """doctor always prints JSON and exits 1 when a connectivity check fails."""
        data, result = self.run_json(["doctor"], allow_failure=True)
        if not isinstance(data, dict):
            raise WorkctlError("`workctl doctor` did not print a JSON object", result)
        return data, result

    def endpoint_init(
        self,
        kind: str,
        profile: str,
        *,
        url: str = "",
        auth: str = "",
        username: str = "",
        backend: str = "",
        projects: Sequence[str] = (),
        default_project: str = "",
    ) -> Result:
        """`workctl jira|gitlab|github init --non-interactive`: writes the profile, never a token."""
        args = [kind, "init", "--non-interactive", "--profile", profile]
        for flag, value in (("--url", url), ("--auth", auth), ("--user", username), ("--backend", backend),
                            ("--default-project", default_project)):
            if value:
                args += [flag, value]
        if projects:
            args += ["--projects", ",".join(projects)]
        return self.run(args)

    def set_token(self, profile: str, secret: str) -> Result:
        """`workctl auth set-token --profile NAME --stdin`, the secret on stdin."""
        value = secret.strip()
        if not value:
            raise WorkctlError("the token is empty")
        if "\n" in value or "\r" in value:
            raise WorkctlError("the token must be a single line")
        return self.run(["auth", "set-token", "--profile", profile, "--stdin"], stdin=value + "\n")

    def login(self, profile: str, on_line: Callable[[str], None], *, no_browser: bool = False) -> int:
        args = ["auth", "login", "--profile", profile] + (["--no-browser"] if no_browser else [])
        return self.stream(args, on_line)


def _failure(result: Result) -> str:
    message = result.stderr.strip() or result.stdout.strip() or f"exit code {result.returncode}"
    return f"`workctl {' '.join(result.command)}` failed: {message}"
