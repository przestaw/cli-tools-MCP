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
