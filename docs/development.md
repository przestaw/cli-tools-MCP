# Development

Author: [przestaw](https://github.com/przestaw)

## Build and test

For the code layout and design decisions see [ARCHITECTURE.md](../ARCHITECTURE.md).

```powershell
go vet ./... ; go test ./... ; go build -trimpath -ldflags "-s -w" -o workctl.exe .\cmd\workctl
```

`scripts/release.sh` cross-compiles the binaries into `dist/` with a `SHA256SUMS` file; publishing them is a manual step.

Go 1.26. Third-party modules come in only through the two built-in modules (cobra, go-keyring, go-prompt, survey, yaml).
The azure and atlassian packages' own tests run too; run them with `APPDATA`/`USERPROFILE` pointing at a scratch directory if you
want to be sure nothing touches your real config.

## Versioning

`X.Y.Z`, kept equal in `internal/app/app.go`, the User-Agent in `internal/api/client.go` and
`plugins/workctl/.claude-plugin/plugin.json`:

- `Z`: everything stored in the OS keyring stays readable as it is (backward compatible).
- `Y`: the keyring layout changes and existing entries need `scripts/migrate.py`.
- `X`: reserved; stays `0` until the toolset covers what the agents need.
