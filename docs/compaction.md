# Optional API response compaction

The first slice on `feat/compaction` adds the same opt-in presentation to MCP `request` and CLI `api raw`:

```sh
workctl --profile gitlab api raw --output compact GET version
```

MCP: `request` with `output: "compact"`. `output: "original"` or omission retains the existing response.
The legacy `compact: true` remains HTML-only; contradictory options fail before sending a request.
The new option also works with `response_metadata: true` without losing Location or bypassing binary checks.

| Data | Current compact behavior |
|---|---|
| JSON | Remove insignificant whitespace without decoding numbers or selecting fields |
| HTML | DOM-to-text conversion; table cells of a row are separated with ` | `; link addresses and form controls are not kept; fragment detection also uses Content-Type |
| XML | Unchanged bytes, including mixed content and namespaces |
| Other text | Unchanged |
| Binary | MCP request refuses it; use download. CLI raw preserves bytes. |

Invalid declared JSON gives an explicit conversion error. This does not undo the HTTP operation: never replay
a write merely to get a different presentation. CLI `api raw` without the option remains byte-preserving.
PR projection is separate: `pr list|show --format raw` retains provider details; descriptions remain in both modes.

## Remaining work, intentionally not enabled

- Semantic HTML renderer preserving tables, links, code and safe form information. The existing text renderer
  is lossy and does not reconstruct browser/CSS state.
- Retention and paginated retrieval of the same original response before aggressive truncation.
- TOON only after number/type round-trip and tokenizer benchmarks.
- XML compaction for concrete schemas; no generic XML-to-JSON conversion or whitespace stripping.
- Compact presentations for normalized list tools, without changing their default response contracts.

No runtime tokenizer, new MCP tool, RTK dependency or external conversion service was added.
