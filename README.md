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
- **Noise Reduction for Scrape**: Built-in `excludeTags` selector filtering removes common non-content blocks (navigation, ads, sidebars, comments, etc.) to improve signal quality.
- **Flexible Environment Variable Configuration**: Supports fine-tuned service configuration via environment variables.
- **The Search and Scrape Endpoints perform some request pre-processing and post-processing, which can save quite a few tokens.**

## Available Tools

This service provides the following tools:

| Tool Name                | Description                                  |
| ------------------------ | -------------------------------------------- |
| `firecrawl-search`  | Performs general Google web / news / images searches.        |
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
| `FIRECRAWL_API_KEY` | fc-xxx | your-firecrawl-api-key-here |
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

## Tool Parameters and Usage Examples

### firecrawl-search: Perform web / news / images search

Parameters:

- `query` (str, required): Keywords to search.
- `country` (str, optional): Specify the country/region for search results. Supports Chinese names (e.g., "China"), English names (e.g., "United States"), or ISO codes (e.g., "US"). Default is "US".
- `search_num` (int, optional): Number of results to return, range 1-100. Default is 20.
- `search_time` (str, optional): Filter results by time range. Available values: "hour", "day", "week", "month", "year".

Example:

```Python
result_json = firecrawl_search(
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
{"success":true,"data":{"web":[{"title":"Example Web","description":"Example description","url":"https://example.com"}],"news":[],"images":[{"title":"Example Image","imageUrl":"https://img.example.com/1.jpg","url":"https://example.com/image"}]},"creditsUsed":3}
```

### firecrawl-scrape: Scrape webpage content

Parameters:

- `url` (str, required): URL of the target webpage.

Example:

```Python
result_json = firecrawl_scrape(
    url="https://www.example.com"
)
```

Built-in noise filtering:

- The tool uses an internal `excludeTags` selector set to suppress noisy DOM regions and prioritize main content quality.

Response (mapped):

- Top-level fields: `success`, `proxyUsed`, `title`, `description`, `language`, `markdown`, `creditsUsed`
- `markdown` is URL-decoded before returning to the client
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
- This is a breaking response-contract change for consumers that relied on the old full passthrough structure (`query_details` + `results`).

## License Agreement

This project is licensed under the MIT License.


