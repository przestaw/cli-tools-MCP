$ErrorActionPreference = "Stop"
Push-Location "$PSScriptRoot\.."
go test ./...
go build -trimpath -ldflags "-s -w" -o workctl.exe ./cmd/workctl
Pop-Location
