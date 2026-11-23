# Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
# This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

"""The three tabs: profiles, doctor and config."""

from __future__ import annotations

import copy
from collections.abc import Sequence

from PySide6.QtCore import Qt, Signal
from PySide6.QtGui import QColor
from PySide6.QtWidgets import (
    QCheckBox,
    QComboBox,
    QFormLayout,
    QGroupBox,
    QHBoxLayout,
    QHeaderView,
    QLabel,
    QLineEdit,
    QMessageBox,
    QPushButton,
    QTableWidget,
    QTableWidgetItem,
    QVBoxLayout,
    QWidget,
)

from . import actions
from .cli import Result, Workctl
from .dialogs import AddProfileDialog, DiffDialog, LoginDialog, ask_secret, log_view
from .models import (
    PROJECT_KINDS,
    REPO_KINDS,
    Account,
    ConfigDocument,
    DoctorCheck,
    parse_accounts,
    parse_doctor,
    split_list,
)
from .tasks import run_task

GOOD = QColor("#1b7f3b")
BAD = QColor("#b00020")


def make_table(headers: Sequence[str]) -> QTableWidget:
    table = QTableWidget(0, len(headers))
    table.setHorizontalHeaderLabels(list(headers))
    table.setEditTriggers(QTableWidget.EditTrigger.NoEditTriggers)
    table.setSelectionBehavior(QTableWidget.SelectionBehavior.SelectRows)
    table.setSelectionMode(QTableWidget.SelectionMode.SingleSelection)
    table.setWordWrap(True)
    table.verticalHeader().setVisible(False)
    table.horizontalHeader().setSectionResizeMode(QHeaderView.ResizeMode.Interactive)
    table.horizontalHeader().setStretchLastSection(True)
    return table


def fit_columns(table: QTableWidget, max_width: int = 360) -> None:
    """Sizes columns to their contents, but keeps long messages from pushing the others out of view."""
    table.resizeColumnsToContents()
    last = table.columnCount() - 1
    for col in range(last):
        table.setColumnWidth(col, min(table.columnWidth(col), max_width))
    used = sum(table.columnWidth(col) for col in range(last))
    table.setColumnWidth(last, max(table.viewport().width() - used, max_width))
    table.resizeRowsToContents()


def fill_row(table: QTableWidget, row: int, values: Sequence[str], color: QColor | None = None, column: int = -1) -> None:
    for col, value in enumerate(values):
        item = QTableWidgetItem(value)
        item.setToolTip(value)
        if color is not None and col == column:
            item.setForeground(color)
        table.setItem(row, col, item)


class ProfilesView(QWidget):
    """`workctl accounts`: every profile with its credential state (never the secret)."""

    profiles_changed = Signal()

    def __init__(self, workctl: Workctl) -> None:
        super().__init__()
        self.workctl = workctl
        self.accounts: list[Account] = []
        self.status = QLabel()
        self.table = make_table(("Name", "Kind", "Host / URL", "Backend / auth", "Username", "Default", "Bound to", "Credential"))
        buttons = QHBoxLayout()
        for label, slot in (("Refresh", self.refresh), ("Add profile…", self.add_profile),
                            ("Set token…", self.set_token), ("Log in…", self.login)):
            button = QPushButton(label)
            button.clicked.connect(slot)
            buttons.addWidget(button)
        buttons.addStretch(1)
        layout = QVBoxLayout(self)
        layout.addLayout(buttons)
        layout.addWidget(self.table, 1)
        layout.addWidget(self.status)

    def refresh(self) -> None:
        self.status.setText("Running workctl accounts…")
        run_task(lambda _: parse_accounts(self.workctl.accounts()), self._show, self._error)

    def _show(self, parsed: tuple[list[Account], bool]) -> None:
        self.accounts, allow_write = parsed
        self.table.setRowCount(len(self.accounts))
        for row, a in enumerate(self.accounts):
            fill_row(self.table, row, (
                a.name, a.kind, a.host or a.url, f"{a.backend} / {a.auth}", a.username,
                "yes" if a.default else "", a.bindings(), a.credential,
            ), GOOD if a.credential_ok() else BAD, 7)
        fit_columns(self.table)
        writes = "allowed" if allow_write else "blocked (read-only)"
        self.status.setText(f"{len(self.accounts)} profile(s); writes {writes}.")

    def _error(self, message: str) -> None:
        self.status.setText("ERROR: " + message)

    def selected(self) -> Account | None:
        rows = self.table.selectionModel().selectedRows()
        if not rows:
            QMessageBox.information(self, "workctl", "Select a profile first.")
            return None
        return self.accounts[rows[0].row()]

    def add_profile(self) -> None:
        dialog = AddProfileDialog(self.workctl, self)
        dialog.exec()
        if dialog.created:
            self.refresh()
            self.profiles_changed.emit()

    def set_token(self) -> None:
        account = self.selected()
        if account is None:
            return
        what = "OAuth2 client_secret" if account.auth == "oauth2" else "Token"
        secret = ask_secret(self, f"Set token: {account.name}", f"{what} for {account.name} (sent to workctl on stdin):")
        if secret is None:
            return
        self.status.setText(f"Storing the credential of {account.name}…")
        run_task(lambda _: self.workctl.set_token(account.name, secret), self._token_stored, self._error)

    def _token_stored(self, result: Result) -> None:
        output = result.output()
        if result.ok:
            self.status.setText(output.splitlines()[0] if output else "Stored.")
            self.refresh()
        else:
            self._error(output or f"exit code {result.returncode}")

    def login(self) -> None:
        account = self.selected()
        if account is None:
            return
        if not account.needs_login():
            QMessageBox.information(self, "workctl", f"{account.name} keeps a token: use Set token.")
            return
        LoginDialog(self.workctl, account.name, self).exec()
        self.refresh()


class DoctorView(QWidget):
    """`workctl doctor`: one row per check, with a fix hint for the failures."""

    def __init__(self, workctl: Workctl) -> None:
        super().__init__()
        self.workctl = workctl
        self.status = QLabel("Doctor contacts every configured service; press Run.")
        self.table = make_table(("Status", "Check", "Code", "Fix hint", "Message"))
        self.run_button = QPushButton("Run doctor")
        self.run_button.clicked.connect(self.refresh)
        top = QHBoxLayout()
        top.addWidget(self.run_button)
        top.addStretch(1)
        layout = QVBoxLayout(self)
        layout.addLayout(top)
        layout.addWidget(self.table, 1)
        layout.addWidget(self.status)

    def refresh(self) -> None:
        self.run_button.setEnabled(False)
        self.status.setText("Running workctl doctor…")
        run_task(lambda _: parse_doctor(self.workctl.doctor()[0]), self._show, self._error)

    def _show(self, checks: list[DoctorCheck]) -> None:
        self.run_button.setEnabled(True)
        self.table.setRowCount(len(checks))
        for row, c in enumerate(checks):
            fill_row(self.table, row, ("ok" if c.ok else "FAIL", c.name, c.code, c.hint, c.message), GOOD if c.ok else BAD, 0)
        fit_columns(self.table)
        failed = sum(not c.ok for c in checks)
        self.status.setText(f"{len(checks) - failed} ok, {failed} failed.")

    def _error(self, message: str) -> None:
        self.run_button.setEnabled(True)
        self.status.setText("ERROR: " + message)


class ConfigView(QWidget):
    """Edits policy, defaults and repo/project bindings of config.json; shows the diff before saving."""

    POLICY = (
        ("allow_write", "Allow writes (comments, pull requests, …)"),
        ("confirm_writes", "Confirm every MCP write"),
        ("confirm_session_writes", "Confirm the first MCP write of each session"),
    )
    BINDINGS = (("repos", "Repositories"), ("default_repo", "Default repository"),
                ("projects", "Projects"), ("default_project", "Default project"))

    def __init__(self, workctl: Workctl) -> None:
        super().__init__()
        self.workctl = workctl
        self.doc: ConfigDocument | None = None
        self.current_profile = ""
        self.path_label = QLabel()
        self.path_label.setTextInteractionFlags(Qt.TextInteractionFlag.TextSelectableByMouse)
        self.status = QLabel()
        self.status.setWordWrap(True)

        self.policy = {key: QCheckBox(label) for key, label in self.POLICY}
        self.policy["confirm_writes"].toggled.connect(lambda on: on and self.policy["confirm_session_writes"].setChecked(False))
        self.policy["confirm_session_writes"].toggled.connect(lambda on: on and self.policy["confirm_writes"].setChecked(False))
        policy_box = QGroupBox("Policy")
        policy_layout = QVBoxLayout(policy_box)
        for box in self.policy.values():
            policy_layout.addWidget(box)

        self.defaults_box = QGroupBox("Default profile per kind")
        self.defaults_form = QFormLayout(self.defaults_box)
        self.defaults: dict[str, QComboBox] = {}

        self.profile = QComboBox()
        self.profile.currentTextChanged.connect(self._profile_selected)
        self.bindings = {key: QLineEdit() for key, _ in self.BINDINGS}
        bindings_box = QGroupBox("Profile bindings")
        bindings_form = QFormLayout(bindings_box)
        bindings_form.addRow("Profile", self.profile)
        for key, label in self.BINDINGS:
            bindings_form.addRow(label, self.bindings[key])

        buttons = QHBoxLayout()
        for label, slot in (("Reload", self.reload), ("Review and save…", self.review)):
            button = QPushButton(label)
            button.clicked.connect(slot)
            buttons.addWidget(button)
        buttons.addStretch(1)

        self.output = log_view()
        self.output.setMaximumHeight(140)
        layout = QVBoxLayout(self)
        layout.addWidget(self.path_label)
        layout.addWidget(policy_box)
        layout.addWidget(self.defaults_box)
        layout.addWidget(bindings_box)
        layout.addLayout(buttons)
        layout.addWidget(self.status)
        layout.addWidget(self.output)

    def reload(self) -> None:
        self.status.setText("Locating the config file…")
        run_task(lambda _: ConfigDocument.load(self.workctl.config_path()), self._loaded, self._error)

    def _error(self, message: str) -> None:
        self.status.setText("ERROR: " + message)

    def _loaded(self, doc: ConfigDocument) -> None:
        self.doc = doc
        self.path_label.setText(f"Config file: {doc.path}" + ("" if doc.original_text is not None else " (does not exist yet)"))
        policy = doc.section("policy")
        for key, box in self.policy.items():
            box.setChecked(bool(policy.get(key, False)))

        while self.defaults_form.rowCount():
            self.defaults_form.removeRow(0)
        self.defaults = {}
        by_kind: dict[str, list[str]] = {}
        for name, p in sorted(doc.profiles().items()):
            kind = "api" if p.get("kind") == "google" else str(p.get("kind", ""))
            by_kind.setdefault(kind, []).append(name)
        defaults = doc.section("defaults")
        for kind in sorted(set(by_kind) | set(defaults)):
            combo = QComboBox()
            combo.addItem("")
            combo.addItems(by_kind.get(kind, []))
            if defaults.get(kind) and combo.findText(defaults[kind]) < 0:
                combo.addItem(defaults[kind])  # a dangling default stays visible; saving reports it
            combo.setCurrentText(defaults.get(kind, ""))
            self.defaults[kind] = combo
            self.defaults_form.addRow(kind, combo)

        self.current_profile = ""
        self.profile.blockSignals(True)
        self.profile.clear()
        self.profile.addItems(sorted(doc.profiles()))
        self.profile.blockSignals(False)
        self._profile_selected(self.profile.currentText())
        self.status.setText("Loaded. Edit, then Review and save.")

    def _profile_selected(self, name: str) -> None:
        if self.doc is None:
            return
        self._store_bindings()
        self.current_profile = name
        p = self.doc.profiles().get(name, {})
        kind = p.get("kind", "")
        for key, edit in self.bindings.items():
            value = p.get(key, "")
            edit.setText(", ".join(value) if isinstance(value, list) else str(value))
            repo = key in ("repos", "default_repo")
            edit.setEnabled(bool(name) and (kind in REPO_KINDS if repo else kind in PROJECT_KINDS))

    def _store_bindings(self) -> None:
        if self.doc is None or self.current_profile not in self.doc.profiles():
            return
        p = self.doc.profiles()[self.current_profile]
        for key, edit in self.bindings.items():
            if not edit.isEnabled():
                continue
            if key in ("repos", "projects"):
                values = split_list(edit.text())
                if values != (p.get(key) or []):
                    self.doc.set_list(self.current_profile, key, values)
            elif edit.text().strip() != p.get(key, ""):
                self.doc.set_text(self.current_profile, key, edit.text().strip())

    def edited(self) -> ConfigDocument | None:
        """A copy of the loaded document with the edits of the form applied."""
        if self.doc is None:
            return None
        self._store_bindings()
        doc = copy.deepcopy(self.doc)
        policy = doc.section("policy")
        for key, box in self.policy.items():
            if box.isChecked() != bool(policy.get(key, False)):
                doc.set_policy(key, box.isChecked())
        defaults = doc.section("defaults")
        for kind, combo in self.defaults.items():
            if combo.currentText() != defaults.get(kind, ""):
                doc.set_default(kind, combo.currentText())
        return doc

    def review(self) -> None:
        doc = self.edited()
        if doc is None:
            return
        if not doc.changed():
            self.status.setText("No changes.")
            return
        if DiffDialog(doc.diff(), doc.problems(), self).exec() != DiffDialog.DialogCode.Accepted:
            return
        self.output.clear()
        self.status.setText("Saving and letting workctl load the new file…")
        run_task(lambda log: self._save(doc, log), self._saved, self._save_failed, self.output.appendPlainText)

    def _save(self, doc: ConfigDocument, log: actions.LineSink) -> None:
        actions.save_and_check(self.workctl, doc, log)
        accounts = self.workctl.run(["accounts"])
        log("$ workctl accounts: " + ("ok" if accounts.ok else accounts.output()))

    def _saved(self, _: object) -> None:
        self.status.setText("Saved; workctl loads the new config.")
        self.reload()

    def _save_failed(self, message: str) -> None:
        self.status.setText("ERROR: " + message)
        self.reload()
