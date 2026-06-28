package main

import (
	"fmt"
	"io"
)

func printRootUsage(w io.Writer) {
	fmt.Fprint(w, `Usage:
  firecrawl aggregated --query <keywords> [--country <country>] [--search-num <1-100>] [--search-time <hour|day|week|month|year>] [--timeout <seconds>] [--proxy <url>]
  firecrawl web        --query <keywords> [--country <country>] [--search-num <1-100>] [--search-time <hour|day|week|month|year>] [--timeout <seconds>] [--proxy <url>]
  firecrawl news       --query <keywords> [--country <country>] [--search-num <1-100>] [--search-time <hour|day|week|month|year>] [--timeout <seconds>] [--proxy <url>]
  firecrawl image      --query <keywords> [--country <country>] [--search-num <1-100>] [--search-time <hour|day|week|month|year>] [--timeout <seconds>] [--proxy <url>]
  firecrawl scholar    --query <keywords> [--search-num <1-500>] [--categories <categories>] [--time-from <date>] [--time-to <date>] [--timeout <seconds>] [--proxy <url>]
  firecrawl scrape     --output <name> [--path <dir>] --url <url> [--include-tags <selectors>] [--exclude-tags <selectors>] [--empty-tags] [--scroll] [--skip-tls] [--headers <json-object>] [--headers-file <file>] [--timeout <seconds>] [--proxy <url>]
  firecrawl parse      (--url <url> | --file <file>) --output <name> [--path <dir>] [--skip-tls] [--timeout <seconds>] [--proxy <url>]
  firecrawl audio-scrape --url <url> [--timeout <seconds>] [--proxy <url>]
  firecrawl video-scrape --url <url> [--timeout <seconds>] [--proxy <url>]
  firecrawl credit-usage [--json] [--pretty] [--proxy <url>]

The API key is read from FIRECRAWL_KEY.
The optional API base URL is read from FIRECRAWL_BASE_URL and defaults to https://api.firecrawl.dev/v2.

`)
}

func printScholarUsage(w io.Writer) {
	fmt.Fprint(w, `Usage:
  firecrawl scholar --query <keywords> [--search-num <1-500>] [--categories <categories>] [--time-from <date>] [--time-to <date>] [--timeout <seconds>] [--proxy <url>]

Parameters:
  --query       Research paper search keywords. Required. Minimum length is 1.
  --search-num  Number of papers to return. Optional. Legal range: 1-500. Default is 5.
  --categories  Comma-separated paper category filters. Optional. All filters must match.
  --time-from   Inclusive created/updated date lower bound. Optional. Format: yyyy-MM-dd, for example 2000-05-28.
  --time-to     Inclusive created/updated date upper bound. Optional. Format: yyyy-MM-dd, for example 2026-06-28.
  --timeout     Request timeout in seconds. Optional. Must be > 0. Default is 120.
  --proxy       Proxy URL for requests to Firecrawl API. Optional. Supports http, https, socks4, socks4a, socks5, and socks5h.

Output:
  Compact single-line JSON with success and data.scholar.

`)
}

func printParseUsage(w io.Writer) {
	fmt.Fprint(w, `Usage:
  firecrawl parse (--url <url> | --file <file>) --output <name> [--path <dir>] [--skip-tls] [--timeout <seconds>] [--proxy <url>]

Parameters:
  --url       Target document URL. Required unless --file is provided.
  --file      Local document file. Required unless --url is provided. Supported extensions: .html, .htm, .pdf, .docx, .doc, .odt, .rtf, .xlsx, .xls.
  --output    Export name. Required. The result is saved as <output>.md.
  --path      Directory where the markdown export is saved. Optional. Supports absolute and relative paths. Default is the current directory.
  --skip-tls  Skip TLS certificate verification for URL parsing. Optional. Default is false.
  --timeout   Request timeout in seconds. Optional. Must be > 0. Default is 120.
  --proxy     Proxy URL for requests to Firecrawl API. Optional. Supports http, https, socks4, socks4a, socks5, and socks5h.

Output:
  true on success. The output directory is created before parsing, and the markdown export is written only after a successful parse.
  false followed by an error reason on failure. Existing files are not created or overwritten on failure.

`)
}

func printAVScrapeUsage(w io.Writer, commandName string, format string) {
	fmt.Fprintf(w, `Usage:
  firecrawl %s --url <url> [--timeout <seconds>] [--proxy <url>]

Parameters:
  --url      Target audio/video webpage URL. Required.
  --timeout  Request timeout in seconds. Optional. Must be > 0. Default is 120.
  --proxy    Proxy URL for requests to Firecrawl API. Optional. Supports http, https, socks4, socks4a, socks5, and socks5h.

Output:
  Compact single-line JSON with creditsUsed, title, description, %s, and success.

`, commandName, format)
}

func printSearchUsage(w io.Writer, name string) {
	fmt.Fprintf(w, `Usage:
  firecrawl %s --query <keywords> [--country <country>] [--search-num <1-100>] [--search-time <hour|day|week|month|year>] [--timeout <seconds>] [--proxy <url>]

Parameters:
  --query        Search keywords. Required.
  --country      Country or region for search results. Optional. Supports names and ISO codes. Default is US.
  --search-num   Number of results to return. Optional. Legal range: 1-100. Default is 20.
  --search-time  Time filter. Optional. One of: "hour", "day", "week", "month", "year".
  --timeout      Request timeout in seconds. Optional. Must be > 0. Default is 120.
  --proxy        Proxy URL for requests to Firecrawl API. Optional. Supports http, https, socks4, socks4a, socks5, and socks5h.

Output:
  Compact single-line JSON with success, data.web, data.news, data.images, and creditsUsed.

`, name)
}

func printScrapeUsage(w io.Writer) {
	fmt.Fprint(w, `Usage:
  firecrawl scrape --output <name> [--path <dir>] --url <url> [--include-tags <selectors>] [--exclude-tags <selectors>] [--empty-tags] [--scroll] [--skip-tls] [--headers <json-object>] [--headers-file <file>] [--timeout <seconds>] [--proxy <url>]

Parameters:
  --output          Export name. Required. The result is saved as <output>.md.
  --path            Directory where the markdown export is saved. Optional. Supports absolute and relative paths. Default is the current directory.
  --url             Target webpage URL. Required.
  --include-tags    CSS selectors to include. Optional. Single selector, comma-separated string, or JSON string array.
  --exclude-tags    Additional CSS selectors to exclude. Optional. Single selector, comma-separated string, or JSON string array.
  --empty-tags      Clear the built-in exclude selector list while keeping user-provided --exclude-tags.
  --scroll          Enable wait and scroll actions before scraping.
  --skip-tls        Skip TLS certificate verification for the upstream scrape target. Optional. Default is false.
  --headers         Root-level request headers as a JSON object, for example {"Authorization":"Bearer token","X-Trace-Id":"abc123"}.
  --headers-file    Path to a headers file. Supports JSON headers/cookies, HTTP header string, Netscape cookies, or Cookie header value.
  --timeout         Request timeout in seconds. Optional. Must be > 0. Default is 120.
  --proxy           Proxy URL for requests to Firecrawl API. Optional. Supports http, https, socks4, socks4a, socks5, and socks5h.

Input examples:
  --include-tags "article"
  --exclude-tags ".nav,.footer,#sidebar"
  --include-tags '["main article",".post-content","#content"]'

Common CSS selector examples:
  --include-tags '["article",".content","#main"]'
  --include-tags '["[data-testid=\"article-body\"]","[class*=\"content\"]","[id^=\"post-\"]"]'
  --include-tags '["main article","main > article","article.post"]'
  --exclude-tags '["nav[aria-label=\"Breadcrumb\"]","aside.related",".promo-banner"]'
  --include-tags '["article:has(h1, h2)",".content"]'

Notes:
  Use a JSON string array when a selector itself contains commas, spaces, or quotes.

Output:
  true on success. The output directory is created before scraping, and the markdown export is written only after a successful scrape.
  false followed by an error reason on failure. Existing files are not created or overwritten on failure.

`)
}

func printCreditUsageUsage(w io.Writer) {
	fmt.Fprint(w, `Usage:
  firecrawl credit-usage [--json] [--pretty] [--proxy <url>]

Parameters:
  --json    Output JSON. Optional. JSON is the default output format.
  --pretty  Pretty-print JSON output. Optional.
  --proxy   Proxy URL for requests to Firecrawl API. Optional. Supports http, https, socks4, socks4a, socks5, and socks5h.

Output:
  JSON response from /v2/team/credit-usage.

`)
}
