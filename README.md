# Firecrawl MCP Toolkit

A high-performance, asynchronous MCP server that provides comprehensive Google search and web content scraping capabilities through the Firecrawl API (excluding some rarely used interfaces).

This project is built on `httpx`, utilizing asynchronous clients and connection pool management to offer LLMs a stable and efficient external information retrieval tool.

## PyPI Package

firecrawl-toolkit: https://pypi.org/project/firecrawl-toolkit/

## Key Features

- **Asynchronous Architecture**: Fully based on `asyncio` and `httpx`, ensuring high throughput and non-blocking I/O operations.
- **HTTP Connection Pool**: Manages and reuses TCP connections through a global `httpx.AsyncClient` instance, significantly improving performance under high concurrency.
- **Concurrency Control**: Built-in global and per-API endpoint concurrency semaphores effectively manage API request rates to prevent exceeding rate limits.
- **Automatic Retry Mechanism**: Integrated request retry functionality with exponential backoff strategy automatically handles temporary network fluctuations or server errors, enhancing service stability.
- **Intelligent Country Code Parsing**: Includes a comprehensive country name dictionary supporting inputs in Chinese, English, ISO Alpha-2/3, and other formats, with automatic normalization.
- **Response Field Mapping**: Search/Scrape responses are normalized into minimal, client-facing JSON schemas instead of upstream passthrough payloads.
- **Noise Reduction for Scrape**: Built-in `excludeTags` selector filtering removes common non-content blocks (navigation, ads, sidebars, comments, etc.) to improve signal quality. Supports returning a specified Markdown character window with `startIndex` and `maxCharacters`.
- **Flexible Environment Variable Configuration**: Supports fine-tuned service configuration via environment variables.
- **The Search and Scrape Endpoints perform some request pre-processing and post-processing, which can save quite a few tokens.**

## Available Tools

This service provides the following tools:

| Tool Name                | Description                                  |
| ------------------------ | -------------------------------------------- |
| `firecrawl-aggregated-search`  | Aggregated Search Interface, Combining Webpage, News, And Image Search Results.          |
| `firecrawl-web-search`          | Web Search Interface. |
| `firecrawl-news-search`          | News Search Interface. |
| `firecrawl-image-search`          | Image Search Interface. |
| `firecrawl-scrape`          | Scrapes and returns the content of a specified URL. |

## Installation Guide

It is recommended to install using `pip` or `uv`.

```bash
# Using pip
pip install firecrawl-toolkit

# Or using uv
uv pip install firecrawl-toolkit
```

## Quick Start

### Set Environment Variables

Create a `.env` file in the project root directory and enter your Firecrawl API key:

| Environment Variables | Default value | Description |
| :---: | :---: | :--- |
| `FIRECRAWL_API_KEY` | fc-xxx | Your Firecrawl API key. Multiple keys can be separated by commas, and one will be selected randomly for each request. |
| `FIRECRAWL_HTTP2` | 0 | Disable or enable HTTP2, <0/1> |
| `FIRECRAWL_MAX_WORKERS` | 10 | Number of processes |
| `FIRECRAWL_MAX_CONNECTIONS` | 200 | Maximum number of connections |
| `FIRECRAWL_MAX_CONCURRENT_REQUESTS` | 200 | Maximum number of concurrent requests |
| `FIRECRAWL_KEEPALIVE` | 20 | Maximum number of concurrent connections |
| `FIRECRAWL_RETRY_COUNT` | 3 | Maximum number of retries |
| `FIRECRAWL_RETRY_BASE_DELAY` | 0.5 | Base delay time for retries in seconds |
| `FIRECRAWL_ENDPOINT_CONCURRENCY` | `{"search":10,"scrape":2}` | Set concurrency per endpoint (JSON format) |
| `FIRECRAWL_ENDPOINT_RETRYABLE` | `{"scrape": false}` | Set retry allowance per endpoint (JSON format) |
| `FIRECRAWL_MCP_ENABLE_STDIO` | 0 | Disable or enable STDIO, <0/1> |
| `FIRECRAWL_MCP_ENABLE_HTTP` | 0 | Disable or enable HTTP, <0/1> |
| `FIRECRAWL_MCP_ENABLE_SSE` | 0 | Disable or enable SSE, <0/1> |
| `FIRECRAWL_MCP_HTTP_HOST` | 127.0.0.1 | HTTP host address |
| `FIRECRAWL_MCP_HTTP_PORT` | 7001 | HTTP host port |
| `FIRECRAWL_MCP_SSE_HOST` | 127.0.0.1 | SSE host address |
| `FIRECRAWL_MCP_SSE_PORT` | 7001 | SSE host port |
| `FIRECRAWL_MCP_LOCK_FILE` | `/tmp/firecrawl_mcp.lock` | Lock file path |

- **STDIO, HTTP, and SSE can only be used one at a time.** If you need to use multiple protocols, please start separate services for each.
- When using multiple services, please specify different lock files for each.

### Configure MCP Client

Add the following server configuration in the MCP client configuration file:

```json
{
  "mcpServers": {
    "firecrawl": {
      "command": "python3",
      "args": ["-m", "firecrawl-toolkit"],
      "env": {
        "FIRECRAWL_API_KEY": "<Your Firecrawl API key>"
      }
    }
  }
}
```

```json
{
  "mcpServers": {
    "firecrawl": {
      "command": "uvx",
      "args": ["firecrawl-toolkit"],
      "env": {
        "FIRECRAWL_API_KEY": "<Your Firecrawl API key>"
      }
    }
  }
}
```

## Go CLI

The Go CLI is located in the `cli` directory. It is a standalone command-line client named `firecrawl`, separate from the Python MCP server.

The CLI reads the API key only from `FIRECRAWL_KEY`:

```bash
export FIRECRAWL_KEY="<Your Firecrawl API key>"
```

### Build From Source

Build for the current platform:

```bash
cd cli
go test ./...
go build -o firecrawl .
```

Run it directly after building:

```bash
./firecrawl --help
```

Build all release targets locally:

```bash
cd cli
mkdir -p dist

CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o dist/firecrawl_windows_amd64.exe .
CGO_ENABLED=0 GOOS=windows GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o dist/firecrawl_windows_arm64.exe .
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o dist/firecrawl_linux_amd64 .
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o dist/firecrawl_linux_arm64 .
CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o dist/firecrawl_darwin_amd64 .
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o dist/firecrawl_darwin_arm64 .
```

### CLI Search Usage

Search commands:

```bash
firecrawl aggregated --query "AI advancements 2024" --country "United States" --search-num 5 --search-time month
firecrawl web --query "AI advancements 2024" --country US --search-num 5
firecrawl news --query "OpenAI news" --search-time week
firecrawl image --query "firecrawl logo" --search-num 10
```

Search command parameters:

- `--query` (required): Search keywords.
- `--country` (optional): Country or region name / ISO code. Default is `US`.
- `--search-num` (optional): Number of results, range `1`-`100`. Default is `20`.
- `--search-time` (optional): One of `hour`, `day`, `week`, `month`, `year`.

Search commands output compact single-line JSON, using the same mapped fields as the Python search tools:

```json
{"success":true,"data":{"web":[],"news":[],"images":[]},"creditsUsed":1}
```

### CLI Credit Usage

Check team credit usage:

```bash
firecrawl credit-usage
firecrawl credit-usage --pretty
```

Credit usage command parameters:

- `--json` (optional): Output JSON. JSON is the default output format.
- `--pretty` (optional): Pretty-print JSON output.

Default output is compact JSON:

```json
{"success":true,"data":{"remainingCredits":1000,"planCredits":500000,"billingPeriodStart":"2025-01-01T00:00:00Z","billingPeriodEnd":"2025-01-31T23:59:59Z"}}
```

### CLI Scrape Usage

Scrape a page and save the markdown export as `example.md` in the current directory:

```bash
firecrawl scrape \
  --output example \
  --url "https://www.example.com" \
  --include-tags '["article",".content"]' \
  --exclude-tags ".nav,.footer" \
  --start-index 0 \
  --max-characters 1200 \
  --headers '{"X-Trace-Id":"abc123"}'
```

`--include-tags` and `--exclude-tags` accept these input forms:

```bash
# Single selector
firecrawl scrape --output page --url "https://www.example.com" --include-tags "article"

# Comma-separated selector list
firecrawl scrape --output page --url "https://www.example.com" --exclude-tags ".nav,.footer,#sidebar"

# JSON string array, recommended when selectors contain spaces, quotes, or commas
firecrawl scrape --output page --url "https://www.example.com" --include-tags '["main article",".post-content","#content"]'
```

Common CSS selector types:

```bash
# Tag, class, and ID selectors
firecrawl scrape --output page --url "https://www.example.com" --include-tags '["article",".content","#main"]'

# Attribute selectors with square brackets
firecrawl scrape --output page --url "https://www.example.com" --include-tags '["[data-testid=\"article-body\"]","[class*=\"content\"]","[id^=\"post-\"]"]'

# Descendant, child, and compound selectors
firecrawl scrape --output page --url "https://www.example.com" --include-tags '["main article","main > article","article.post"]'

# Exclusion selectors
firecrawl scrape --output page --url "https://www.example.com" --exclude-tags '["nav[aria-label=\"Breadcrumb\"]","aside.related",".promo-banner"]'

# Selectors that contain commas must use a JSON string array
firecrawl scrape --output page --url "https://www.example.com" --include-tags '["article:has(h1, h2)",".content"]'
```

Scrape command parameters:

- `--output` (required): Export name. The CLI writes `<output>.md` in the current directory.
- `--url` (required): Target webpage URL.
- `--include-tags` (optional): CSS selectors to include. Accepts a single selector, comma-separated selector string, or JSON string array.
- `--exclude-tags` (optional): Additional CSS selectors to exclude. Accepts a single selector, comma-separated selector string, or JSON string array.
- `--start-index` (optional): Markdown truncation start index. Must be `>= 0`. Default is `0`.
- `--max-characters` (optional): Maximum markdown characters from `--start-index`. Must be `> 0` when provided.
- `--headers` (optional): JSON object with string values, for example `{"Authorization":"Bearer token","X-Trace-Id":"abc123"}`.

Scrape output:

- On success, stdout is `true`, and the CLI writes `<output>.md`.
- On failure, stdout is `false` followed by the error reason, and no file is created or overwritten.

The generated markdown file uses this structure:

```markdown
## title:
## description:
## language:
## creditsUsed:

---

markdown content
```

## Tool Parameters and Usage Examples

### firecrawl Search: Perform aggregated / web / news / images search

Parameters:

- `query` (str, required): Keywords to search.
- `country` (str, optional): Specify the country/region for search results. Supports Chinese names (e.g., "China"), English names (e.g., "United States"), or ISO codes (e.g., "US"). Default is "US".
- `search_num` (int, optional): Number of results to return, range 1-100. Default is 20.
- `search_time` (str, optional): Filter results by time range. Available values: "hour", "day", "week", "month", "year".

Example:

```Python
result_json = firecrawl_web_search(
    query="AI advancements 2024",
    country="United States",
    search_num=5,
    search_time="month"
)
```

Response (mapped):

- Top-level fields: `success`, `data`, `creditsUsed`
- `data.web[]`: `title`, `description`, `url`
- `data.news[]`: `title`, `snippet`, `url`, `date`
- `data.images[]`: `title`, `imageUrl`, `url`
- `web` / `news` / `images` remain arrays and may be empty (`[]`)
- Missing mapped fields are preserved as `null`
- Output is compact single-line JSON (no extra spaces)

Example response:

```json
{"success":true,"data":{"web":[{"title":"Example Web","description":"Example description","url":"https://example.com"}],"news":[],"images":[]},"creditsUsed":1}
```

### firecrawl-scrape: Scrape webpage content

Parameters:

- `url` (str, required): URL of the target webpage.
- `excludeTags` (list[str], optional, default `[]`): Additional CSS selectors to exclude; merged with built-in noise-filter selectors after normalization and deduplication unless `emptyTags=True`.
- `includeTags` (list[str], optional, default `None`): Additional CSS selectors to include; no built-in defaults are applied, and the cleaned list is forwarded only when this parameter is provided.
- `maxCharacters` (int, optional, default `None`): Truncate only the returned `markdown` to N characters starting at `startIndex`. Invalid values (non-int, `<= 0`) are ignored and treated as not provided.
- `startIndex` (int, optional, default `0`): Start offset used with `maxCharacters` when slicing returned `markdown`. Invalid values (non-int, `< 0`) are treated as `0`.
- `emptyTags` (bool, optional, default `False`): Clear the built-in exclude selector list for this request, while still keeping any user-provided `excludeTags`.
- `headers` (dict[str, str], optional, default `None`): Root-level request headers passed through to the upstream scrape request only when a non-empty object is provided.

Example:

```Python
result_json = firecrawl_scrape(
    url="https://www.example.com",
    includeTags=["article", ".content"],
    excludeTags=["[class^=\"skip\"]", "[id*=\"disqus\"]"],
    startIndex=0,
    maxCharacters=1200,
    headers={"Authorization": "Bearer token", "X-Trace-Id": "abc123"}
)
```
This returns at most 1200 characters in `markdown`, starting at character index 0.

To explicitly send an empty include selector list:

```Python
result_json = firecrawl_scrape(
    url="https://www.example.com",
    includeTags=[]
)
```

To disable only the built-in exclude selectors for one request:

```Python
result_json = firecrawl_scrape(
    url="https://www.example.com",
    emptyTags=True
)
```

To disable the built-in exclude selectors but keep your own:

```Python
result_json = firecrawl_scrape(
    url="https://www.example.com",
    excludeTags=[".nav"],
    emptyTags=True
)
```

Built-in noise filtering:

- The tool uses an internal `excludeTags` selector set to suppress noisy DOM regions and prioritize main content quality.
- `includeTags` has no built-in defaults and is only forwarded when explicitly provided.
- Passing `emptyTags=True` clears only the built-in exclude selector set for that request.
- If the first scrape returns `data.markdown == ""`, the tool automatically retries once without `includeTags`/`excludeTags` as a fallback.
- `startIndex` / `maxCharacters` slicing is applied locally in this toolkit post-processing and is not forwarded to upstream Firecrawl payloads.

Response (mapped):

- Top-level fields: `success`, `proxyUsed`, `title`, `description`, `language`, `markdown`, `creditsUsed`
- `markdown` is URL-decoded before returning to the client
- When a valid `maxCharacters` is provided, `markdown` length is capped at that value after applying `startIndex`
- Missing mapped fields are preserved as `null`
- Output is compact single-line JSON (no extra spaces)

Example response:

```json
{"success":true,"proxyUsed":"auto","title":"Example Page","description":"Example summary","language":"en","markdown":"Hello world!","creditsUsed":1}
```

## Response Contract Notes

- `firecrawl-search` and `firecrawl-scrape` success payloads are mapped to stable minimal schemas.
- Missing mapped fields are preserved as `null` (arrays remain arrays, and may be empty).
- Both success and error responses are compact single-line JSON.

## License Agreement

This project is licensed under the GNU General Public License v3.0 or later (GPL-3.0-or-later).



