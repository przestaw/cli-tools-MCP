param([switch]$VSCode) # also register the MCP server in VS Code
$ErrorActionPreference = "Stop"
$dest = if ($env:WORKCTL_INSTALL_DIR) { $env:WORKCTL_INSTALL_DIR } else { Join-Path $HOME ".local\bin" }
Push-Location "$PSScriptRoot\.."
try {
    New-Item -ItemType Directory -Force -Path $dest | Out-Null
    go build -trimpath -ldflags "-s -w" -o (Join-Path $dest "workctl.exe") .
} finally { Pop-Location }
Write-Host "installed: $dest\workctl.exe"
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
