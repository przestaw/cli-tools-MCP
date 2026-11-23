# Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
# This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

from PySide6.QtCore import QThreadPool
from PySide6.QtWidgets import QApplication

from workctl_gui.app import MainWindow
from workctl_gui.cli import Workctl


def settle(app: QApplication) -> None:
    for _ in range(3):
        QThreadPool.globalInstance().waitForDone(10000)
        app.processEvents()


def test_main_window_builds_and_loads(workctl: Workctl) -> None:
    app = QApplication.instance() or QApplication([])
    window = MainWindow(workctl)
    window.show()
    settle(app)
    assert window.profiles.table.rowCount() == 2
    assert window.config_view.doc is not None
    window.doctor.refresh()
    settle(app)
    assert window.doctor.table.rowCount() == 3
    assert window.statusBar().currentMessage().startswith("workctl 0.4.8")
    window.close()
