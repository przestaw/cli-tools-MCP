# Optional RTK companion

RTK filters local command output; workctl handles authenticated provider APIs. Neither depends on the other.
The native workctl plugin includes an opt-in `compact-output` skill alongside its provider skill. RTK itself is
not vendored, downloaded by the plugin, or registered as an MCP server. No hooks are installed.
The existing standalone installer still installs only the workctl skill; installation of the companion in
Codex/Copilot skill directories remains an explicit separate step, not a claimed universal plugin installer.

## Evaluation

Tested RTK **OSS 1.2.0**, Linux x86_64 musl, using synthetic Go tests with Go 1.26.11.
The release archive's SHA-256 was verified against the GitHub release asset digest:

```text
a28407dc13038bdc9c2a1f5a047b0c636e0c1d495b15523576e2adb992a06a17
```

The archive includes Apache-2.0 LICENSE. Choose the OSS asset rather than the combined edition with the
BUSL engine. Any redistribution still needs the applicable license/notice files.
RTK was evaluated in isolated config/data/cache directories, with telemetry disabled and no hooks.

| Synthetic case | Plain Go JSON bytes | RTK summary bytes | Plain / RTK exit | Observation |
|---|---:|---:|---|---|
| 41 passing tests including parent | 28151 | 33 | 0 / 0 | Counts retained |
| Assertion failure | 1103 | 197 | 1 / 1 | Message and source line retained |
| Panic | 3333 | 376 | 1 / 1 | Panic message retained, stack shortened |
| Timeout | 5966 | 129 | 1 / 1 | **Cause omitted from summary** |

Separate synthetic executions produced these measurements, so timing fields differ. These are byte counts,
not tokenizer measurements or end-to-end savings. The compact command internally collects Go JSON; comparison
is against that representation, not a claim that every short plain `go test` output becomes smaller.
Explicit `rtk go test -json` passed JSON through rather than compacting it.

`rtk recall HASH --full` recovered the timeout cause and omitted panic source location. No command was rerun
to recover those details. `rtk pipe --filter go-test` also filtered an already saved JSON log.
The fallback/readback round trip must be included in future token benchmarks.

## Decision

Suitable as an **optional, explicit companion**, not a transparent default for every command.
Do not adopt an automatic hook until failure completeness and host permission behavior are validated.
The companion skill requires recovery of omitted failure details.

Not evaluated: actual Codex/Claude Code/Copilot plugin loading, compiler/linker diagnostics, signal forwarding,
interactive stdin/TTY, Windows/macOS, other RTK filters, or model-token counts.
No global RTK installation, hook, proxy or telemetry setting was changed by this evaluation.

## Bundling path

1. Keep the two skills in the native plugin; RTK remains an optional external executable.
2. Extend the standalone installer with an explicit RTK companion option only after installation/retention
   policy is settled. Detect an existing RTK without replacing it.
3. Verify the selected client's skill discovery location. Plugin manifests and hooks are client-specific.
4. Before automatic use, test errors, timeout, signals and recall retention on that exact version and host.
5. Do not route workctl MCP responses through RTK: API compaction stays in workctl.

Sources: [release](https://github.com/rtk-ai/rtk/releases/tag/v1.2.0),
[editions](https://www.rtk-ai.app/docs/resources/editions/),
[installation](https://www.rtk-ai.app/docs/getting-started/installation/).
