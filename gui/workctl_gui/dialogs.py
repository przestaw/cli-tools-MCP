# Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
# This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

"""Dialogs: add a profile, enter a secret, follow a login, review a config diff."""

from __future__ import annotations

from PySide6.QtGui import QFontDatabase
from PySide6.QtWidgets import (
    QCheckBox,
    QComboBox,
    QDialog,
    QDialogButtonBox,
    QFormLayout,
    QInputDialog,
    QLabel,
    QLineEdit,
    QPlainTextEdit,
    QVBoxLayout,
    QWidget,
)

from . import actions
from .cli import Workctl
from .models import AUTH_SCHEMES, BACKENDS, DEFAULT_URLS, GOOGLE_SERVICES, KINDS, NewProfile, split_list
from .tasks import run_task


def log_view() -> QPlainTextEdit:
    view = QPlainTextEdit()
    view.setReadOnly(True)
    view.setFont(QFontDatabase.systemFont(QFontDatabase.SystemFont.FixedFont))
    return view


def ask_secret(parent: QWidget, title: str, label: str) -> str | None:
    """A masked one-line input; the caller passes the value to workctl on stdin and drops it."""
    text, ok = QInputDialog.getText(parent, title, label, QLineEdit.EchoMode.Password)
    return text if ok and text.strip() else None


class AddProfileDialog(QDialog):
    """Creates a profile with workctl (or a direct config edit where workctl has no command) and stores its credential."""

    def __init__(self, workctl: Workctl, parent: QWidget | None = None) -> None:
        super().__init__(parent)
        self.workctl = workctl
        self.created = False
        self.setWindowTitle("Add profile")
        self.resize(640, 640)

        self.name = QLineEdit()
        self.kind = QComboBox()
        self.kind.addItems(KINDS)
        self.url = QLineEdit()
        self.auth = QComboBox()
        self.backend = QComboBox()
        self.username = QLineEdit()
        self.auth_header = QLineEdit()
        self.auth_header.setPlaceholderText("X-Api-Key")
        self.service = QComboBox()
        self.service.addItems(GOOGLE_SERVICES)
        self.client_id = QLineEdit()
        self.repos = QLineEdit()
        self.repos.setPlaceholderText("owner/name, owner/*")
        self.projects = QLineEdit()
        self.projects.setPlaceholderText("group/project or PROJ")
        self.make_default = QCheckBox("Use as the default profile of its kind")
        self.secret = QLineEdit()
        self.secret.setEchoMode(QLineEdit.EchoMode.Password)
        self.start_login = QCheckBox("Start `workctl auth login` after creating (browser opens outside the GUI)")
        self.start_login.setChecked(True)

        self.form = QFormLayout()
        self.form.addRow("Profile name", self.name)
        self.form.addRow("Kind", self.kind)
        self.form.addRow("URL", self.url)
        self.form.addRow("Backend", self.backend)
        self.form.addRow("Authentication", self.auth)
        self.form.addRow("Username", self.username)
        self.form.addRow("Auth header", self.auth_header)
        self.form.addRow("Google service", self.service)
        self.form.addRow("OAuth2 client_id", self.client_id)
        self.form.addRow("Repositories", self.repos)
        self.form.addRow("Projects", self.projects)
        self.form.addRow("", self.make_default)
        self.form.addRow("Token", self.secret)
        self.form.addRow("", self.start_login)

        self.note = QLabel()
        self.note.setWordWrap(True)
        self.output = log_view()
        self.buttons = QDialogButtonBox(QDialogButtonBox.StandardButton.Close)
        self.create_button = self.buttons.addButton("Create", QDialogButtonBox.ButtonRole.ActionRole)
        self.create_button.clicked.connect(self._create)
        self.buttons.rejected.connect(self.reject)

        layout = QVBoxLayout(self)
        layout.addLayout(self.form)
        layout.addWidget(self.note)
        layout.addWidget(self.output, 1)
        layout.addWidget(self.buttons)

        self.kind.currentTextChanged.connect(self._kind_changed)
        self.auth.currentTextChanged.connect(self._update)
        self.backend.currentTextChanged.connect(self._update)
        self._kind_changed(self.kind.currentText())

    def _kind_changed(self, kind: str) -> None:
        self.auth.blockSignals(True)
        self.auth.clear()
        self.auth.addItems([a for a in AUTH_SCHEMES[kind] if a])
        self.auth.blockSignals(False)
        self.backend.blockSignals(True)
        self.backend.clear()
        self.backend.addItems(BACKENDS.get(kind, ()))
        self.backend.blockSignals(False)
        if not self.url.text() or self.url.text() in DEFAULT_URLS.values():
            self.url.setText(DEFAULT_URLS.get(kind, ""))
        self._update()

    def _update(self) -> None:
        kind, auth, backend = self.kind.currentText(), self.auth.currentText(), self.backend.currentText()
        cli = backend == "cli"
        self.form.setRowVisible(self.backend, kind in BACKENDS)
        self.form.setRowVisible(self.auth, self.auth.count() > 1 and not cli)
        self.form.setRowVisible(self.username, not cli and kind != "google")
        self.form.setRowVisible(self.auth_header, auth == "header")
        self.form.setRowVisible(self.service, kind == "google")
        self.form.setRowVisible(self.client_id, kind == "google" or auth == "oauth2")
        self.form.setRowVisible(self.repos, kind in ("github", "gitlab", "azure"))
        self.form.setRowVisible(self.projects, kind in ("gitlab", "jira", "azure", "atlassian"))
        oauth = kind == "google" or auth == "oauth2"
        self.form.setRowVisible(self.secret, not cli and auth != "none")
        self.form.setRowVisible(self.start_login, oauth)
        label = self.form.labelForField(self.secret)
        if isinstance(label, QLabel):
            label.setText("OAuth2 client_secret (optional)" if oauth else "Password" if auth in ("basic", "form") else "Token")
        if kind in ("github", "gitlab", "jira"):
            how = f"The profile is created with `workctl {kind} init --non-interactive`."
        else:
            how = f"workctl has no command that adds a {kind} profile: the GUI writes config.json itself (backup in config.json.bak)."
        if cli:
            how += f" The {'gh' if kind == 'github' else 'glab'} CLI must already be logged in; the profile is bound to that account."
        else:
            how += " The secret goes to `workctl auth set-token --stdin` and is not kept by the GUI."
        self.note.setText(how)

    def _spec(self) -> NewProfile:
        backend = self.backend.currentText() if self.form.isRowVisible(self.backend) else ""
        auth = self.auth.currentText() if backend != "cli" else ""
        return NewProfile(
            name=self.name.text().strip(),
            kind=self.kind.currentText(),
            url=self.url.text().strip(),
            auth=auth,
            username=self.username.text().strip() if self.form.isRowVisible(self.username) else "",
            backend=backend,
            repos=tuple(split_list(self.repos.text())) if self.form.isRowVisible(self.repos) else (),
            projects=tuple(split_list(self.projects.text())) if self.form.isRowVisible(self.projects) else (),
            service=self.service.currentText() if self.kind.currentText() == "google" else "",
            client_id=self.client_id.text().strip() if self.form.isRowVisible(self.client_id) else "",
            auth_header=self.auth_header.text().strip() if auth == "header" else "",
            make_default=self.make_default.isChecked(),
        )

    def _create(self) -> None:
        spec = self._spec()
        if not spec.name:
            self.output.appendPlainText("Enter a profile name.")
            return
        secret = self.secret.text() if self.form.isRowVisible(self.secret) else ""
        self.secret.clear()
        start_login = self.start_login.isChecked() and spec.needs_login()
        self.create_button.setEnabled(False)
        self.output.appendPlainText(f"Creating profile {spec.name} ({spec.kind})...")
        run_task(
            lambda log: actions.create_profile(self.workctl, spec, secret, log, start_login),
            self._done,
            self._failed,
            self.output.appendPlainText,
        )

    def _done(self, _: object) -> None:
        self.created = True
        self.output.appendPlainText("Done.")
        self.create_button.setEnabled(True)

    def _failed(self, message: str) -> None:
        self.output.appendPlainText("ERROR: " + message)
        self.create_button.setEnabled(True)


class LoginDialog(QDialog):
    """Follows `workctl auth login --profile NAME`; the browser or device flow happens outside the GUI."""

    def __init__(self, workctl: Workctl, profile: str, parent: QWidget | None = None) -> None:
        super().__init__(parent)
        self.setWindowTitle(f"Log in: {profile}")
        self.resize(640, 360)
        self.output = log_view()
        buttons = QDialogButtonBox(QDialogButtonBox.StandardButton.Close)
        buttons.rejected.connect(self.reject)
        layout = QVBoxLayout(self)
        layout.addWidget(QLabel("Complete the login in the browser or in the provider CLI. Output of workctl:"))
        layout.addWidget(self.output, 1)
        layout.addWidget(buttons)
        self.output.appendPlainText(f"$ workctl auth login --profile {profile}")
        run_task(
            lambda log: workctl.login(profile, log),
            lambda code: self.output.appendPlainText("Done." if code == 0 else f"Failed (exit code {code})."),
            lambda message: self.output.appendPlainText("ERROR: " + message),
            self.output.appendPlainText,
        )


class DiffDialog(QDialog):
    """Shows the change to config.json; accepting it means save."""

    def __init__(self, diff: str, problems: list[str], parent: QWidget | None = None) -> None:
        super().__init__(parent)
        self.setWindowTitle("Review changes")
        self.resize(720, 480)
        view = log_view()
        view.setPlainText(diff or "(no changes)")
        layout = QVBoxLayout(self)
        if problems:
            warning = QLabel("Cannot save:\n" + "\n".join(problems))
            warning.setStyleSheet("color: #b00020")
            layout.addWidget(warning)
        layout.addWidget(view, 1)
        buttons = QDialogButtonBox(QDialogButtonBox.StandardButton.Save | QDialogButtonBox.StandardButton.Cancel)
        buttons.button(QDialogButtonBox.StandardButton.Save).setEnabled(bool(diff) and not problems)
        buttons.accepted.connect(self.accept)
        buttons.rejected.connect(self.reject)
        layout.addWidget(buttons)
