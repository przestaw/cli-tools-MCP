# Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
# This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

"""Main window and entry point: `python -m workctl_gui [--workctl PATH] [--config FILE]`."""

from __future__ import annotations

import argparse
import sys
from collections.abc import Sequence

from PySide6.QtWidgets import (
    QApplication,
    QHBoxLayout,
    QLabel,
    QLineEdit,
    QMainWindow,
    QPushButton,
    QTabWidget,
    QVBoxLayout,
    QWidget,
)

from .cli import Workctl
from .tasks import run_task
from .views import ConfigView, DoctorView, ProfilesView


class MainWindow(QMainWindow):
    def __init__(self, workctl: Workctl) -> None:
        super().__init__()
        self.workctl = workctl
        self.setWindowTitle("workctl")
        self.resize(1100, 680)

        self.binary = QLineEdit(workctl.binary)
        self.config = QLineEdit(workctl.config or "")
        self.config.setPlaceholderText("default: the file `workctl config path` names")
        apply = QPushButton("Apply")
        apply.clicked.connect(self.apply_settings)
        settings = QHBoxLayout()
        settings.addWidget(QLabel("workctl binary"))
        settings.addWidget(self.binary, 2)
        settings.addWidget(QLabel("config"))
        settings.addWidget(self.config, 2)
        settings.addWidget(apply)

        self.profiles = ProfilesView(workctl)
        self.doctor = DoctorView(workctl)
        self.config_view = ConfigView(workctl)
        self.profiles.profiles_changed.connect(self.config_view.reload)
        self.tabs = QTabWidget()
        self.tabs.addTab(self.profiles, "Profiles")
        self.tabs.addTab(self.doctor, "Doctor")
        self.tabs.addTab(self.config_view, "Config")

        central = QWidget()
        layout = QVBoxLayout(central)
        layout.addLayout(settings)
        layout.addWidget(self.tabs, 1)
        self.setCentralWidget(central)
        self.refresh()

    def apply_settings(self) -> None:
        self.workctl.binary = self.binary.text().strip() or self.workctl.binary
        self.workctl.config = self.config.text().strip() or None
        self.refresh()

    def refresh(self) -> None:
        self.statusBar().showMessage(f"Using {self.workctl.binary}…")
        run_task(
            lambda _: self.workctl.version(),
            lambda version: self.statusBar().showMessage(f"workctl {version} ({self.workctl.binary})"),
            lambda message: self.statusBar().showMessage("ERROR: " + message),
        )
        self.profiles.refresh()
        self.config_view.reload()


def main(argv: Sequence[str] | None = None) -> int:
    parser = argparse.ArgumentParser(prog="workctl-gui", description="Desktop GUI for workctl profiles, doctor and config.")
    parser.add_argument("--workctl", help="workctl binary (default: $WORKCTL_GUI_BINARY, else workctl on PATH)")
    parser.add_argument("--config", help="config file passed to workctl as --config")
    args, qt_args = parser.parse_known_args(argv)
    app = QApplication([sys.argv[0], *qt_args])
    window = MainWindow(Workctl(args.workctl, args.config))
    window.show()
    return app.exec()
