# Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
# This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

# Builds workctl into $env:WORKCTL_INSTALL_DIR (default ~\.local\bin) and registers it with the agents you name:
#   -VSCode  MCP server in the VS Code user profile
#   -Claude  MCP server in Claude Code (user scope) and the skill in ~\.claude\skills (VS Code reads it there too)
#   -Codex   MCP server in Codex
# A server that is already registered under the name "workctl" is left as it is.
param([switch]$VSCode, [switch]$Claude, [switch]$Codex)
$ErrorActionPreference = "Stop"
$dest = if ($env:WORKCTL_INSTALL_DIR) { $env:WORKCTL_INSTALL_DIR } else { Join-Path $HOME ".local\bin" }
Push-Location "$PSScriptRoot\.."
try {
    New-Item -ItemType Directory -Force -Path $dest | Out-Null
    go build -trimpath -ldflags "-s -w" -o (Join-Path $dest "workctl.exe") ./cmd/workctl
} finally { Pop-Location }
Write-Host "installed: $dest\workctl.exe"
$python = (Get-Command python -ErrorAction SilentlyContinue), (Get-Command py -ErrorAction SilentlyContinue) | Where-Object { $_ } | Select-Object -First 1
if ($python) {
    # Copies v2 keyring entries (<kind>:<url>) to the v3 accounts (<kind>:<profile>); no-op when none exist.
    & $python.Source "$PSScriptRoot\migrate.py"
} else {
    Write-Host "python not found: if tokens were stored by the v2 release, run scripts/migrate.py later"
}
$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
if (($userPath -split ";") -notcontains $dest) {
    [Environment]::SetEnvironmentVariable("Path", "$userPath;$dest", "User")
    Write-Host "added $dest to the user PATH (restart the terminal)"
}
if ($VSCode) {
    $server = @{ name = "workctl"; type = "stdio"; command = (Join-Path $dest "workctl.exe"); args = @("mcp") } | ConvertTo-Json -Compress
    # The URL handler avoids the quoting problems of passing JSON through code.cmd.
    Start-Process ("vscode:mcp/install?" + [uri]::EscapeDataString($server))
    Write-Host "confirm 'Install' in VS Code to add the workctl MCP server to your user profile"
}
if ($Claude) {
    # cmd /c keeps stderr of the agent CLI from stopping this script.
    cmd /c "claude mcp get workctl >nul 2>nul"
    if ($LASTEXITCODE -eq 0) {
        Write-Host "Claude Code already has an MCP server named workctl: left as it is"
    } else {
        claude mcp add --scope user workctl -- (Join-Path $dest "workctl.exe") mcp
        if ($LASTEXITCODE -ne 0) { throw "claude mcp add failed" }
    }
    $skill = Join-Path $HOME ".claude\skills\workctl"
    New-Item -ItemType Directory -Force -Path $skill | Out-Null
    Copy-Item "$PSScriptRoot\..\plugins\workctl\skills\workctl\SKILL.md" $skill -Force
    Write-Host "workctl skill installed in $skill"
}
if ($Codex) {
    $listed = cmd /c "codex mcp list 2>nul" | Select-String -Quiet -Pattern '^\s*workctl(\s|$)'
    if ($listed) {
        Write-Host "Codex already lists a server named workctl: left as it is"
    } else {
        codex mcp add workctl -- (Join-Path $dest "workctl.exe") mcp
        if ($LASTEXITCODE -ne 0) { throw "codex mcp add failed" }
    }
}
