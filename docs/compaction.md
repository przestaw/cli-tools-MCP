# Optional response compaction

Compaction saves LLM tokens. It is always opt-in for raw requests; for `pr_list` and `pr_show` the short `details`
projection is the default and `raw` opts out. Compaction happens locally, after the HTTP call.

## When to use it

| Situation | Use |
|---|---|
| Reading pages of older, server-rendered web applications: dashboards, ticket or build overviews, login pages (HTML of 10-150 KB) | `output: compact`. Typical result is 5% of the original size. |
| JSON that a server pretty-prints (indented output) | `output: compact` saves about 20%. |
| JSON that is already minified (most provider APIs) | Nothing to gain; use a normalized tool (`pr_list`, `pr_show`, `ci`) or a `fields` parameter instead. |
| Markup matters (forms, link targets, tokens, exact layout) | The original response. |

Navigation menus of a page stay in the text; the result is a reading aid, not a main-content extractor.

## Raw requests: `request` and `api raw`

```sh
workctl --profile gitlab api raw --output compact GET version
```

MCP: `request` with `output: "compact"`. `output: "original"` or omission keeps the response as received.
The legacy `compact: true` (MCP only) means "HTML only": an HTML page becomes text, JSON and everything else stay
as sent. `compact: true` with `output: "compact"` is the same as `output: "compact"`; `compact: true` with
`output: "original"`, or an unknown `output`, fails before any request is sent (CLI: before the write policy check too).
`response_metadata: true` works with both: `body` is the compacted text and `location` is kept.

| Data | What `output: compact` does | Lossy? |
|---|---|---|
| JSON | Removes whitespace between tokens only. Numbers (`9007199254740993`, `1.2300`, `1E+2`) and string contents are copied byte for byte. | No (key order and values kept) |
| HTML | Replaced by its visible text, see below. | **Yes** |
| XML (`application/xml`, `text/xml`, `+xml`) | Unchanged. | No |
| Other text | Unchanged. | No |
| Binary | MCP `request` refuses it (use `download`); CLI `api raw` writes the bytes unchanged. | No |

A body counts as HTML when its Content-Type is `text/html` or it starts with `<!doctype html`, `<html`, `<head` or `<body`.
A body that is valid JSON is minified whatever its Content-Type. A body declared as JSON that is not valid JSON, or an HTML
page the parser refuses (nesting deeper than 512 levels), is an error. The request was already sent: never replay a write
just to get another presentation.

### What the HTML conversion keeps and drops

Kept: text, with a line break at block elements (`p`, `div`, `br`, `li`, `tr`, headings, `table`, `ul`, `ol`, `section`,
`article`, `blockquote`), `|` between the cells of a row, and the layout (indentation, line breaks) inside `<pre>`.
Whitespace elsewhere collapses to one space and blank lines disappear. Adjacent links, buttons, labels and options
keep a space between them, so `<a>Name</a><a>log out</a>` reads `Name log out`. Entities are decoded.

Dropped: every attribute, so link targets, image `alt`, `value`, `placeholder` and the values of hidden inputs (such as
CSRF tokens) are not in the output; comments; `script`, `style`, `head` (title included), `noscript`, `svg`, `template`;
and the whole content of any element with the HTML `hidden` attribute (any value, including `until-found`). Text the page
shows only in form controls (an `option` label, a `textarea` body) is kept, input values are not. Not kept: links, list
bullets, emphasis, tables as grids, images.

CSS is not evaluated: text hidden with `style="display:none"` or a class stays, and so does `aria-hidden="true"` text
(it is visible to sighted users). This is a size reduction, not a way to remove secrets from a page. For sensitive pages
use the original response and your own judgement. Use the original response also when markup matters.

## Error bodies

When a request fails, the error shows the first 1200 bytes of the response body (cut at a character boundary). An HTML
body is shown as the same visible text, never as markup. If it cannot be converted, a short note replaces it.

## Pull requests: `pr_list`, `pr_show`

`details` of every item keeps only: merge state (`merged`, `merged_at`, `closed_at`, `mergeable`, `mergeable_state`,
`merge_status`, `detailed_merge_status`, `has_conflicts`), `squash`, `sha`, size (`commits`, `changed_files`, `additions`,
`deletions`, `changes_count`), counters (`comments`, `review_comments`, `user_notes_count`), and the names of `labels`,
`assignees`, `reviewers`, `requested_reviewers`. Everything else the provider sent (author objects, URLs, avatars, nested
repositories, time stats) is dropped, including fields not listed here. Title, description, state and branches are not
affected. `format: raw` (CLI `--format raw`) keeps the provider's `details` unchanged. The `include` parts of `pr_show`
follow the same rule: a fixed short shape per part, provider JSON with `format: raw`.

## Not done

- A semantic HTML renderer (links, tables as grids).
- Paged retrieval of the original response.
- TOON or other encodings; XML compaction.
