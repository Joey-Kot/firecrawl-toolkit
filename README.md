# Firecrawl Toolkit

**Turn web pages and PDFs into local, searchable Markdown for agents.**

Firecrawl Toolkit is a CLI-first web capture tool built on top of the Firecrawl API. It is designed for shell-based agents such as Codex, Claude Code, OpenCode, and other local automation workflows.

Instead of dumping long web pages into model context, the CLI saves the page as a local Markdown file and prints only a minimal success/failure signal to stdout.

```bash
firecrawl scrape --url "https://example.com/article" --output article --path ./Temp-Scrape
# true
```

Then let your agent inspect the file with normal local tools:

```bash
rg -n "pricing|risk|governance|download|PDF" ./Temp-Scrape
bat --paging=never --line-range 40:120 ./Temp-Scrape/article.md
```

**Scrape once. Search locally. Keep context clean.**

## Why this toolkit exists

Most web-search or scrape tools treat the web page as immediate model input. That works for short pages, but it breaks down quickly with long reports, news articles, PDFs, documentation pages, and research material.

This toolkit uses a different workflow:

```text
remote URL / PDF
→ local Markdown file
→ rg / bat / sed / awk / local file tools
→ agent reads only the relevant sections
```

This is especially useful when an agent needs to collect multiple sources, compare reports, inspect citations, or build a local research folder.

The goal is not to produce a perfectly clean article-only extraction. The goal is to produce **complete, searchable, agent-readable local source material** with reduced web noise and minimal context pollution.

## Core workflow

### 1. Scrape a web page or PDF into local Markdown

```bash
firecrawl scrape \
  --url "https://example.com/report-or-article" \
  --output report \
  --path ./Temp-Scrape
```

On success:

```text
true
```

The file is saved as:

```text
./Temp-Scrape/report.md
```

The generated Markdown begins with source metadata:

```markdown
## title:
## description:
## url:
## language:
## creditsUsed:

markdown content
```

### 2. Search locally

```bash
rg -n "agentic|governance|pricing|risk|EBIT|download" ./Temp-Scrape
```

### 3. Read only the relevant section

```bash
bat --paging=never --line-range 80:150 ./Temp-Scrape/report.md
```

Or with standard Unix tools:

```bash
sed -n '80,150p' ./Temp-Scrape/report.md
```

## Why stdout is intentionally small

For file-producing commands such as `scrape`, stdout is deliberately minimal.

On success:

```text
true
```

On failure:

```text
false
<short error reason>
```

Large page content is written to disk, not printed to stdout by default. This is intentional: agents should not accidentally ingest a 50k, 100k, or 500k character page into context.

The recommended pattern is:

```text
scrape to file
→ inspect file size / headings / keywords
→ read only the useful ranges
```

## Built-in boilerplate filtering

The scrape command applies a built-in noise filter by default.

It reduces common non-content regions such as:

* scripts, styles, forms, inputs, buttons
* nav bars, headers, footers, asides
* menus and navigation blocks
* logos and brand blocks
* accessibility skip links and visually hidden elements
* ads and advertisement containers
* sidebars
* breadcrumbs and pagination
* related, recommended, and trending sections
* common layout offset/module blocks

This is not meant to aggressively delete every non-article element. The priority is **high recall with reduced noise**: keep the source material useful for local search while removing obvious boilerplate.

Use `--scroll` when a page needs a short wait and body scroll before extraction, so lazy-loaded content has a chance to appear.

If a page contains useful content in an unusual region, you can disable the built-in filter:

```bash
firecrawl scrape \
  --url "https://example.com/page" \
  --output page-raw \
  --empty-tags
```

You can also add your own exclusions:

```bash
firecrawl scrape \
  --url "https://example.com/page" \
  --output page \
  --exclude-tags ".newsletter,.promo,aside.related"
```

## Installation

### Python package

The Python package provides the MCP server.

```bash
uvx firecrawl-toolkit
```

Package:

```text
firecrawl-toolkit
```

### Go CLI

The standalone CLI is located in the `cli` directory.

Build from source:

```bash
cd cli
go test ./...
go build -o firecrawl .
```

Run:

```bash
./firecrawl --help
```

Build Windows example:

```bash
cd cli
go build -o firecrawl.exe .
```

## API key

The Go CLI reads the Firecrawl API key from:

```bash
FIRECRAWL_KEY
```

Linux/macOS:

```bash
export FIRECRAWL_KEY="fc-..."
```

Windows PowerShell:

```powershell
$env:FIRECRAWL_KEY="fc-..."
```

The Python MCP server uses:

```bash
FIRECRAWL_API_KEY
```

## API base URL

By default, both the Go CLI and the Python MCP server use the official Firecrawl API base URL:

```text
https://api.firecrawl.dev/v2
```

For a self-hosted Firecrawl-compatible service, set:

```bash
FIRECRAWL_BASE_URL
```

Linux/macOS:

```bash
export FIRECRAWL_BASE_URL="https://your-firecrawl.example/v2"
```

Windows PowerShell:

```powershell
$env:FIRECRAWL_BASE_URL="https://your-firecrawl.example/v2"
```

## CLI commands

```text
firecrawl aggregated
firecrawl web
firecrawl news
firecrawl image
firecrawl scholar
firecrawl scrape
firecrawl parse
firecrawl audio-scrape
firecrawl video-scrape
firecrawl credit-usage
```

## Common CLI options

### `--proxy`

Optional. Proxy URL for CLI requests to the Firecrawl API. Supported schemes are `http`, `https`, `socks4`, `socks4a`, `socks5`, and `socks5h`.

Use URL userinfo for proxy authentication, or omit it for no authentication:

```bash
firecrawl web --query "AI policy" --proxy "http://user:pass@127.0.0.1:8080"
firecrawl scrape --url "https://example.com" --output example --proxy "socks5://127.0.0.1:1080"
```

## Scrape command

### Basic usage

```bash
firecrawl scrape \
  --url "https://example.com/article" \
  --output article
```

This writes:

```text
article.md
```

### Save into a directory

```bash
firecrawl scrape \
  --url "https://example.com/article" \
  --output article \
  --path ./Temp-Scrape
```

This writes:

```text
./Temp-Scrape/article.md
```

If the directory does not exist, the CLI tries to create it.

### Scrape a PDF

```bash
firecrawl scrape \
  --url "https://example.com/report.pdf" \
  --output report-pdf \
  --path ./Temp-Scrape
```

The CLI requests Markdown output and enables Firecrawl’s PDF parser.

## Scrape options

### `--output`

Required. Export name.

```bash
firecrawl scrape --url "https://example.com" --output example
```

The CLI writes:

```text
example.md
```

If the provided name already ends with `.md`, it is preserved.

### `--path`

Optional. Output directory.

```bash
firecrawl scrape --url "https://example.com" --output example --path ./exports
```

### `--url`

Required. Target web page or PDF URL.

```bash
firecrawl scrape --url "https://example.com" --output example
```

### `--include-tags`

Optional. CSS selectors to include.

Use this when you know the useful content is inside a specific region:

```bash
firecrawl scrape \
  --url "https://example.com" \
  --output page \
  --include-tags "article"
```

Multiple selectors:

```bash
firecrawl scrape \
  --url "https://example.com" \
  --output page \
  --include-tags ".article-body,#content,main"
```

JSON array form is recommended when selectors contain spaces, quotes, or commas:

```bash
firecrawl scrape \
  --url "https://example.com" \
  --output page \
  --include-tags '["main article",".post-content","#content"]'
```

### `--exclude-tags`

Optional. Additional CSS selectors to exclude.

```bash
firecrawl scrape \
  --url "https://example.com" \
  --output page \
  --exclude-tags ".nav,.footer,#sidebar"
```

This is merged with the built-in boilerplate filter.

### `--empty-tags`

Optional. Disable the built-in exclude selector list for this request.

```bash
firecrawl scrape \
  --url "https://example.com" \
  --output page-raw \
  --empty-tags
```

User-provided `--exclude-tags` are still applied:

```bash
firecrawl scrape \
  --url "https://example.com" \
  --output page-custom \
  --empty-tags \
  --exclude-tags ".nav"
```

### `--scroll`

Optional. Enable wait and scroll actions before scraping.

When enabled, scrape sends these actions in the request payload:

```json
[
  {
    "type": "wait",
    "milliseconds": 2
  },
  {
    "type": "scroll",
    "direction": "down",
    "selector": "body"
  }
]
```

Use `--scroll` when a page needs the extra interaction:

```bash
firecrawl scrape \
  --url "https://example.com" \
  --output page \
  --scroll
```

### `--skip-tls`

Optional. Skip TLS certificate verification for the upstream scrape target.

By default, TLS certificate verification is enabled.

```bash
firecrawl scrape \
  --url "https://example.com" \
  --output page \
  --skip-tls
```

### `--headers`

Optional. JSON object of request headers.

```bash
firecrawl scrape \
  --url "https://example.com" \
  --output page \
  --headers '{"X-Trace-Id":"abc123"}'
```

### `--headers-file`

Optional. Path to a headers file. The file is auto-detected as one of these standard formats:

- JSON headers or cookies, including a plain object, `headers`/`cookies` arrays, or browser extension cookie export arrays
- HTTP header string, for example browser-style `Name: value` lines
- Netscape cookie file
- Cookie header value, for example `a=1; b=2`

```bash
firecrawl scrape \
  --url "https://example.com" \
  --output page \
  --headers-file ./headers.txt
```

Use sensitive headers carefully. Avoid passing credentials, cookies, or authorization tokens unless you understand the risk.

### `--timeout`

Optional. Request timeout in seconds. Default is `120`.

```bash
firecrawl scrape \
  --url "https://example.com" \
  --output page \
  --timeout 180
```

### `--proxy`

Optional. Proxy URL for requests from the CLI to the Firecrawl API. Supports `http`, `https`, `socks4`, `socks4a`, `socks5`, and `socks5h`, with URL userinfo authentication or no authentication.

## Scholar command

`scholar` searches research papers and prints compact single-line JSON to stdout.

```bash
firecrawl scholar \
  --query "AI" \
  --search-num 3 \
  --categories "cs.CY" \
  --time-from "2000-05-28" \
  --time-to "2026-06-28"
```

The command sends a GET request to `/v2/search/research/papers` with query string parameters and a JSON body containing `timeout`.

### Scholar options

#### `--query`

Required. Research paper search keywords. Minimum length is `1`.

#### `--search-num`

Optional. Number of papers to return. Legal range is `1` to `500`. Default is `5`.

#### `--categories`

Optional. Comma-separated paper category filters. All filters must match.

#### `--time-from`

Optional. Inclusive lower bound for created/updated date. Format: `yyyy-MM-dd`, for example `2000-05-28`.

#### `--time-to`

Optional. Inclusive upper bound for created/updated date. Format: `yyyy-MM-dd`, for example `2026-06-28`.

#### `--timeout`

Optional. Request timeout in seconds. Default is `120`.

#### `--proxy`

Optional. Proxy URL for requests from the CLI to the Firecrawl API. Supports `http`, `https`, `socks4`, `socks4a`, `socks5`, and `socks5h`.

Output fields:

```json
{"data":{"scholar":[{"abstract":"Paper abstract","paperId":"2581735124241874","primaryId":"arxiv:2307.10057","score":0.956892745058914,"title":"Paper title"}]},"success":true}
```

## Parse command

Use `parse` for documents from a URL or local file. The command writes Markdown to a local `.md` file and prints only `true` or `false` to stdout.

### Parse a document URL

```bash
firecrawl parse \
  --url "https://example.com/report.xlsx" \
  --output report \
  --path ./Temp-Scrape
```

URL mode sends the document URL through the scrape endpoint with Markdown output, PDF parser support, base64 images preserved, and the configured timeout.

### Parse a local file

```bash
firecrawl parse \
  --file ./report.xlsx \
  --output report \
  --path ./Temp-Scrape
```

File mode uploads the local file to the parse endpoint with multipart form data.

Supported local file extensions:

```text
.html .htm .pdf .docx .doc .odt .rtf .xlsx .xls
```

### Parse options

#### `--url`

Target document URL. Required unless `--file` is provided. `--url` and `--file` are mutually exclusive.

```bash
firecrawl parse --url "https://example.com/report.pdf" --output report
```

#### `--file`

Local document file. Required unless `--url` is provided.

```bash
firecrawl parse --file ./report.pdf --output report
```

#### `--output`

Required. Export name. The result is saved as `<output>.md`.

#### `--path`

Optional. Output directory. Defaults to the current directory.

#### `--skip-tls`

Optional. URL mode only. Skip TLS certificate verification for the upstream document URL. Default is false.

#### `--timeout`

Optional. Request timeout in seconds. Default is `120`.

#### `--proxy`

Optional. Proxy URL for requests from the CLI to the Firecrawl API. Supports `http`, `https`, `socks4`, `socks4a`, `socks5`, and `socks5h`.

The generated Markdown begins with:

```markdown
## title:
## url:
## language:
## creditsUsed:

markdown content
```

## Audio and video scrape commands

`audio-scrape` and `video-scrape` request Firecrawl AV extraction and print compact single-line JSON to stdout.

### Audio scrape

```bash
firecrawl audio-scrape \
  --url "https://www.youtube.com/watch?v=dQw4w9WgXcQ" \
  --timeout 60
```

Output fields:

```json
{"creditsUsed":5,"title":"Video title","description":"Video description","audio":"https://storage.example/audio.mp3","success":true}
```

### Video scrape

```bash
firecrawl video-scrape \
  --url "https://www.youtube.com/watch?v=dQw4w9WgXcQ" \
  --timeout 60
```

Output fields:

```json
{"creditsUsed":5,"title":"Video title","description":"Video description","video":"https://storage.example/video.mp4","success":true}
```

Both commands require `--url`. `--timeout` is optional, accepts seconds, defaults to `120`, and is forwarded to Firecrawl as milliseconds. `--proxy` is also supported for CLI requests to the Firecrawl API.

## Recommended agent usage

Give your agent a narrow workflow instead of exposing every scrape option.

### Default capture

```bash
firecrawl scrape --url "$URL" --output "$NAME" --path ./Temp-Scrape
```

### Inspect the captured source

```bash
rg -n "$KEYWORDS" ./Temp-Scrape
bat --paging=never --line-range 1:120 "./Temp-Scrape/$NAME.md"
```

### If the page is noisy

Try adding exclusions:

```bash
firecrawl scrape \
  --url "$URL" \
  --output "$NAME-clean" \
  --path ./Temp-Scrape \
  --exclude-tags ".newsletter,.promo,aside.related"
```

### If the built-in filter removes something useful

Capture a raw version:

```bash
firecrawl scrape \
  --url "$URL" \
  --output "$NAME-raw" \
  --path ./Temp-Scrape \
  --empty-tags
```

## Search commands

Search commands return compact single-line JSON.

```bash
firecrawl aggregated --query "AI governance 2026" --country US --search-num 10
firecrawl web        --query "AI governance 2026" --country US --search-num 10
firecrawl news       --query "OpenAI news" --search-time week
firecrawl image      --query "firecrawl logo" --search-num 10
```

Available search commands:

```text
aggregated  web + news + images
web         web results
news        news results
image       image results
```

### Search options

#### `--query`

Required. Search keywords.

```bash
firecrawl web --query "AI pricing SaaS"
```

#### `--country`

Optional. Country or region name / ISO code. Default is `US`.

```bash
firecrawl web --query "AI policy" --country "United States"
firecrawl web --query "AI policy" --country US
```

#### `--search-num`

Optional. Number of results, from `1` to `100`. Default is `20`.

```bash
firecrawl web --query "AI policy" --search-num 5
```

#### `--search-time`

Optional. Time filter.

Allowed values:

```text
hour
day
week
month
year
```

Example:

```bash
firecrawl news --query "AI regulation" --search-time week
```

#### `--timeout`

Optional. Request timeout in seconds. Default is `120`.

#### `--proxy`

Optional. Proxy URL for requests from the CLI to the Firecrawl API. Supports `http`, `https`, `socks4`, `socks4a`, `socks5`, and `socks5h`.

## Search output

Search commands output compact JSON:

```json
{"success":true,"data":{"web":[],"news":[],"images":[]},"creditsUsed":1}
```

Mapped fields:

```text
data.web[]:
  title
  description
  url

data.news[]:
  title
  snippet
  url
  date

data.images[]:
  title
  imageUrl
  url
```

Search results are intended to help agents discover URLs. For detailed reading, scrape selected URLs into local Markdown files.

Recommended pattern:

```bash
firecrawl web --query "AI trust maturity survey 2026" --search-num 5
firecrawl scrape --url "<selected-url>" --output ai-trust-survey --path ./Temp-Scrape
rg -n "governance|risk|agentic|maturity" ./Temp-Scrape
```

## Credit usage

Check Firecrawl team credit usage:

```bash
firecrawl credit-usage
```

Pretty-print:

```bash
firecrawl credit-usage --pretty
```

`credit-usage` also supports `--proxy` for CLI requests to the Firecrawl API.

Default output is JSON:

```json
{"success":true,"data":{"remainingCredits":1000,"planCredits":500000,"billingPeriodStart":"2025-01-01T00:00:00Z","billingPeriodEnd":"2025-01-31T23:59:59Z"}}
```

## Exit behavior

### Scrape and parse

Success:

```text
true
```

Failure:

```text
false
<error reason>
```

The CLI writes the Markdown file only after a successful scrape or parse. Existing files are not created or overwritten on failure.

### Search, scholar, credit usage, audio scrape, and video scrape

Search, `scholar`, credit usage, `audio-scrape`, and `video-scrape` commands output JSON.

## Example: local research folder

```bash
mkdir -p ./Temp-Scrape

firecrawl scrape \
  --url "https://www.mckinsey.com/capabilities/tech-and-ai/our-insights/tech-forward/state-of-ai-trust-in-2026-shifting-to-the-agentic-era" \
  --output mckinsey-ai-trust-2026 \
  --path ./Temp-Scrape

firecrawl scrape \
  --url "https://www.reuters.com/business/world-at-work/ai-will-lead-labour-shortages-jeff-bezos-says-vivatech-2026-06-17/" \
  --output reuters-bezos-ai-labor \
  --path ./Temp-Scrape

rg -n "agentic|governance|risk|labor shortage|AI" ./Temp-Scrape
bat --paging=never --line-range 1:120 ./Temp-Scrape/mckinsey-ai-trust-2026.md
```

This creates a local source folder that can be searched and revisited without repeatedly fetching or pasting web pages into context.

## Python MCP server

The project also includes a Python MCP server.

Run with:

```bash
uvx firecrawl-toolkit
```

Example MCP client configuration:

```json
{
  "mcpServers": {
    "firecrawl": {
      "command": "uvx",
      "args": ["firecrawl-toolkit"],
      "env": {
        "FIRECRAWL_API_KEY": "<Your Firecrawl API key>",
        "FIRECRAWL_MCP_ENABLE_STDIO": "1"
      }
    }
  }
}
```

MCP environment variables:

| Variable                            | Default                         | Description                                                  |
| ----------------------------------- | ------------------------------- | ------------------------------------------------------------ |
| `FIRECRAWL_API_KEY`                 | `fc-xxx`                        | Firecrawl API key.                                           |
| `FIRECRAWL_BASE_URL`                | `https://api.firecrawl.dev/v2`  | Firecrawl API base URL. Set this for self-hosted services.   |
| `FIRECRAWL_HTTP2`                   | `0`                             | Enable HTTP/2 with `1`.                                      |
| `FIRECRAWL_MAX_WORKERS`             | `10`                            | Number of worker processes.                                  |
| `FIRECRAWL_MAX_CONNECTIONS`         | `200`                           | Maximum HTTP connections.                                    |
| `FIRECRAWL_MAX_CONCURRENT_REQUESTS` | `200`                           | Maximum concurrent requests.                                 |
| `FIRECRAWL_KEEPALIVE`               | `20`                            | Maximum keepalive connections.                               |
| `FIRECRAWL_RETRY_COUNT`             | `3`                             | Maximum retry count.                                         |
| `FIRECRAWL_RETRY_BASE_DELAY`        | `0.5`                           | Base retry delay in seconds.                                 |
| `FIRECRAWL_ENDPOINT_CONCURRENCY`    | `{"search":10,"scrape":2}`      | Per-endpoint concurrency limits.                             |
| `FIRECRAWL_ENDPOINT_RETRYABLE`      | `{"scrape": false}`             | Per-endpoint retry configuration.                            |
| `FIRECRAWL_MCP_ENABLE_STDIO`        | `0`                             | Enable STDIO transport.                                      |
| `FIRECRAWL_MCP_ENABLE_HTTP`         | `0`                             | Enable HTTP transport.                                       |
| `FIRECRAWL_MCP_ENABLE_SSE`          | `0`                             | Enable SSE transport.                                        |
| `FIRECRAWL_MCP_HTTP_HOST`           | `127.0.0.1`                     | HTTP host.                                                   |
| `FIRECRAWL_MCP_HTTP_PORT`           | `7001`                          | HTTP port.                                                   |
| `FIRECRAWL_MCP_SSE_HOST`            | `127.0.0.1`                     | SSE host.                                                    |
| `FIRECRAWL_MCP_SSE_PORT`            | `7001`                          | SSE port.                                                    |
| `FIRECRAWL_MCP_LOCK_FILE`           | `/tmp/firecrawl_mcp.lock`       | Lock file path.                                              |

STDIO, HTTP, and SSE should be used one at a time. Start separate services with different lock files if multiple transports are needed.

## MCP tools

The MCP server provides:

| Tool                          | Description                                      |
| ----------------------------- | ------------------------------------------------ |
| `firecrawl-aggregated-search` | Aggregated web, news, and image search.          |
| `firecrawl-web-search`        | Web search.                                      |
| `firecrawl-news-search`       | News search.                                     |
| `firecrawl-image-search`      | Image search.                                    |
| `firecrawl-scrape`            | Scrape a URL and return mapped Markdown content. |

For local shell-based agents, the Go CLI is usually the simpler and safer interface because it writes large scrape results to files instead of returning them directly to model context.

## Development

Run Go CLI tests:

```bash
cd cli
go test ./...
```

Build the CLI:

```bash
cd cli
go build -o firecrawl .
```

Run Python tests:

```bash
pytest
```

## License

This project is licensed under the GNU General Public License v3.0 or later.
