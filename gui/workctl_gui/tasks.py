# Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
# This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

"""Runs blocking workctl calls on the Qt thread pool and reports back on the GUI thread."""

from __future__ import annotations

from collections.abc import Callable
from typing import Any

from PySide6.QtCore import QObject, QRunnable, QThreadPool, Signal

from .cli import WorkctlError

LineSink = Callable[[str], None]


class _Signals(QObject):
    done = Signal(object)
    failed = Signal(str)
    line = Signal(str)


class Task(QRunnable):
    """Calls fn(emit_line) on a worker thread; its result or error arrives through the signals."""

    def __init__(self, fn: Callable[[LineSink], Any]) -> None:
        super().__init__()
        self.fn = fn
        self.signals = _Signals()  # created on the GUI thread, so the slots run there

    def run(self) -> None:
        try:
            result = self.fn(self.signals.line.emit)
        except WorkctlError as e:
            self.signals.failed.emit(str(e))
        except Exception as e:  # noqa: BLE001 - any failure is shown to the user
            self.signals.failed.emit(f"{type(e).__name__}: {e}")
        else:
            self.signals.done.emit(result)


_running: set[Task] = set()


def run_task(
    fn: Callable[[LineSink], Any],
    on_done: Callable[[Any], None],
    on_failed: Callable[[str], None],
    on_line: LineSink | None = None,
) -> Task:
    task = Task(fn)
    task.setAutoDelete(False)
    _running.add(task)

    def finish(callback: Callable[[Any], None], value: Any) -> None:
        _running.discard(task)
        callback(value)

    task.signals.done.connect(lambda r: finish(on_done, r))
    task.signals.failed.connect(lambda m: finish(on_failed, m))
    if on_line is not None:
        task.signals.line.connect(on_line)
    QThreadPool.globalInstance().start(task)
    return task
