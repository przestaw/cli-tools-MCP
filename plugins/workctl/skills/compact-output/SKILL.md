---
name: compact-output
description: 'Use when explicitly asked to reduce local build, test or CLI log output with RTK. Optional companion to workctl; preserves failure status and retrieves omitted diagnostics. Not for provider API access, private attachments, ML runs, or conversation compaction.'
---

# Compact local command output

This optional skill ships alongside workctl. It uses a separately installed RTK binary; workctl never invokes RTK.
Check the installed version and help. The evaluated baseline is RTK OSS 1.2.0 (Apache-2.0).
If absent, report the missing dependency; do not download software or change hooks without approval.

## Workflow

1. Use workctl for provider data. Do not substitute `rtk gh`, `rtk glab`, `rtk curl` or browser sessions.
2. For an authorized local test command, prefer an explicit supported RTK wrapper, retaining its arguments
   and the repository's existing test selection. Never broaden a command or rerun it just to shorten output.
3. `rtk go test` filters its internally collected Go JSON output. An explicitly requested `-json` is passed
   through in the evaluated version; do not silently remove it. For an already saved Go JSON log, use
   `rtk pipe --filter go-test < LOG`. A pipe's exit code is not the original test exit code.
4. Preserve and report the actual process exit code. A short summary does not establish that all diagnostics
   were retained. RTK OSS 1.2.0 hides the timeout cause in the Go-test summary and shortens panic stacks.
5. On failure, inspect the supplied recovery handle with `rtk recall HASH --full`, using `--from`, `--lines`
   or `--grep` to bound the read. If the summary has no cause or location, recover those before diagnosing.
   Keep reading the saved result rather than rerunning the test. If no original was saved, state that limitation.
6. Do not enable `rtk init`, hooks, telemetry, model proxies, delegation pipelines or project-local filter trust.
   Those require separate approval. Avoid raw logs containing secrets in shared or committed locations.

Unverified commands, interactive shells, signal forwarding, compiler/linker diagnostics and transactional logs
must not receive automatic lossy filtering. Preserve the original; validate a filter before adopting it.
Do not chain multiple compressors or filter an already compact workctl response.
