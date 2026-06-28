package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

const (
	apiKeyEnv          = "FIRECRAWL_KEY"
	baseURLEnv         = "FIRECRAWL_BASE_URL"
	defaultBaseURL     = "https://api.firecrawl.dev/v2"
	defaultTimeoutSecs = 120
	maxTimeoutSecs     = 9223372036
	defaultRetryCount  = 3
	defaultRetryDelay  = 500 * time.Millisecond
	proxyFlagHelp      = "Proxy URL for requests to Firecrawl API. Optional. Supports http, https, socks4, socks4a, socks5, and socks5h."
)

//go:embed data/country_aliases.json
var embeddedData embed.FS

var (
	httpClient = &http.Client{Timeout: 30 * time.Second}
	endpoints  = map[string]string{
		"search":       joinEndpoint(defaultBaseURL, "search"),
		"scholar":      joinEndpoint(defaultBaseURL, "search/research/papers"),
		"scrape":       joinEndpoint(defaultBaseURL, "scrape"),
		"parse":        joinEndpoint(defaultBaseURL, "parse"),
		"credit-usage": joinEndpoint(defaultBaseURL, "team/credit-usage"),
	}
	countryAliases = loadCountryAliases()
)

type cliError struct {
	message string
	code    int
}

func (e cliError) Error() string {
	return e.message
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		var ce cliError
		if errors.As(err, &ce) {
			if ce.message != "" {
				fmt.Fprintln(os.Stderr, ce.message)
			}
			os.Exit(ce.code)
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer, stderr io.Writer) error {
	if len(args) == 0 {
		printRootUsage(stderr)
		return cliError{code: 2}
	}

	switch args[0] {
	case "aggregated":
		return runSearch("aggregated", []string{"web", "news", "images"}, args[1:], stdout, stderr)
	case "web":
		return runSearch("web", []string{"web"}, args[1:], stdout, stderr)
	case "news":
		return runSearch("news", []string{"news"}, args[1:], stdout, stderr)
	case "image":
		return runSearch("image", []string{"images"}, args[1:], stdout, stderr)
	case "scholar":
		return runScholar(args[1:], stdout, stderr)
	case "scrape":
		return runScrape(args[1:], stdout, stderr)
	case "parse":
		return runParse(args[1:], stdout, stderr)
	case "audio-scrape":
		return runAudioScrape(args[1:], stdout, stderr)
	case "video-scrape":
		return runVideoScrape(args[1:], stdout, stderr)
	case "credit-usage":
		return runCreditUsage(args[1:], stdout, stderr)
	case "-h", "--help", "help":
		printRootUsage(stdout)
		return nil
	default:
		printRootUsage(stderr)
		return cliError{message: fmt.Sprintf("unknown subcommand: %s", args[0]), code: 2}
	}
}

func runSearch(name string, sources []string, args []string, stdout io.Writer, stderr io.Writer) error {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	var query string
	var country string
	searchNum := 20
	var searchTime string
	var proxy string
	timeoutSecs := defaultTimeoutSecs
	fs.StringVar(&query, "query", "", "Search keywords. Required.")
	fs.StringVar(&country, "country", "", "Country or region name/ISO code. Optional. Default is US.")
	fs.IntVar(&searchNum, "search-num", 20, "Number of results to return. Optional. Range: 1-100. Default is 20.")
	fs.StringVar(&searchTime, "search-time", "", `Time filter. Optional. One of: "hour", "day", "week", "month", "year".`)
	addProxyFlag(fs, &proxy)
	fs.IntVar(&timeoutSecs, "timeout", defaultTimeoutSecs, "Request timeout in seconds. Optional. Must be > 0. Default is 120.")
	fs.Usage = func() { printSearchUsage(stderr, name) }
	if err := fs.Parse(args); err != nil {
		return cliError{code: 2}
	}
	if strings.TrimSpace(query) == "" {
		fs.Usage()
		return cliError{message: "--query is required", code: 2}
	}
	if fs.NArg() > 0 {
		return cliError{message: fmt.Sprintf("unexpected positional arguments: %s", strings.Join(fs.Args(), " ")), code: 2}
	}
	if searchNum < 1 || searchNum > 100 {
		return cliError{message: "--search-num must be an integer from 1 to 100", code: 2}
	}
	if err := validateTimeoutSecs(timeoutSecs); err != nil {
		return cliError{message: err.Error(), code: 2}
	}
	if err := validateProxyOption(proxy); err != nil {
		return cliError{message: err.Error(), code: 2}
	}
	tbs, err := mapSearchTime(searchTime)
	if err != nil {
		return cliError{message: err.Error(), code: 2}
	}

	payload := buildSearchPayload(query, country, searchNum, sources, timeoutSecs)
	if tbs != "" {
		payload["tbs"] = tbs
	}

	raw, err := firecrawlPostWithRetry("search", payload, timeoutSecs, proxy)
	if err != nil {
		out := compactJSON(map[string]any{
			"success": false,
			"error":   true,
			"message": err.Error(),
		})
		fmt.Fprintln(stdout, out)
		return cliError{code: 1}
	}
	if success, ok := raw["success"].(bool); ok && !success {
		out := compactJSON(map[string]any{
			"success":  false,
			"error":    true,
			"message":  "search request failed, upstream returned success=false",
			"upstream": raw,
		})
		fmt.Fprintln(stdout, out)
		return cliError{code: 1}
	}
	data, ok := raw["data"].(map[string]any)
	if !ok {
		out := compactJSON(map[string]any{
			"success":  false,
			"error":    true,
			"message":  "search request failed, upstream response is missing data object",
			"upstream": raw,
		})
		fmt.Fprintln(stdout, out)
		return cliError{code: 1}
	}

	fmt.Fprintln(stdout, compactJSON(transformSearchResult(raw, data)))
	return nil
}

func runScholar(args []string, stdout io.Writer, stderr io.Writer) error {
	fs := flag.NewFlagSet("scholar", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var query string
	var categories string
	var timeFrom string
	var timeTo string
	var proxy string
	searchNum := 5
	timeoutSecs := defaultTimeoutSecs
	fs.StringVar(&query, "query", "", "Research paper search keywords. Required.")
	fs.IntVar(&searchNum, "search-num", 5, "Number of papers to return. Optional. Range: 1-500. Default is 5.")
	fs.StringVar(&categories, "categories", "", "Comma-separated paper category filters. Optional. All filters must match.")
	fs.StringVar(&timeFrom, "time-from", "", "Inclusive created/updated date lower bound. Optional.")
	fs.StringVar(&timeTo, "time-to", "", "Inclusive created/updated date upper bound. Optional.")
	addProxyFlag(fs, &proxy)
	fs.IntVar(&timeoutSecs, "timeout", defaultTimeoutSecs, "Request timeout in seconds. Optional. Must be > 0. Default is 120.")
	fs.Usage = func() { printScholarUsage(stderr) }
	if err := fs.Parse(args); err != nil {
		return cliError{code: 2}
	}
	if strings.TrimSpace(query) == "" {
		fs.Usage()
		return cliError{message: "--query is required", code: 2}
	}
	if fs.NArg() > 0 {
		return cliError{message: fmt.Sprintf("unexpected positional arguments: %s", strings.Join(fs.Args(), " ")), code: 2}
	}
	if searchNum < 1 || searchNum > 500 {
		return cliError{message: "--search-num must be an integer from 1 to 500", code: 2}
	}
	if err := validateTimeoutSecs(timeoutSecs); err != nil {
		return cliError{message: err.Error(), code: 2}
	}
	if err := validateProxyOption(proxy); err != nil {
		return cliError{message: err.Error(), code: 2}
	}

	raw, err := firecrawlGetWithJSONBodyWithRetry("scholar", buildScholarQuery(query, searchNum, categories, timeFrom, timeTo), buildTimeoutPayload(timeoutSecs), timeoutSecs, proxy)
	if err != nil {
		out := compactJSON(map[string]any{
			"success": false,
			"error":   true,
			"message": err.Error(),
		})
		fmt.Fprintln(stdout, out)
		return cliError{code: 1}
	}
	if success, ok := raw["success"].(bool); ok && !success {
		out := compactJSON(map[string]any{
			"success":  false,
			"error":    true,
			"message":  "scholar request failed, upstream returned success=false",
			"upstream": raw,
		})
		fmt.Fprintln(stdout, out)
		return cliError{code: 1}
	}
	fmt.Fprintln(stdout, compactJSON(transformScholarResult(raw)))
	return nil
}

func runScrape(args []string, stdout io.Writer, stderr io.Writer) error {
	fs := flag.NewFlagSet("scrape", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var output string
	var outputDir string
	var targetURL string
	var includeTags string
	var excludeTags string
	var emptyTags bool
	var scroll bool
	var skipTLS bool
	var headersRaw string
	var headersFile string
	var proxy string
	timeoutSecs := defaultTimeoutSecs
	fs.StringVar(&output, "output", "", "Export name. Required. The result is saved as <output>.md.")
	fs.StringVar(&outputDir, "path", "", "Directory where the markdown export is saved. Optional. Supports absolute and relative paths. Default is the current directory.")
	fs.StringVar(&targetURL, "url", "", "Target webpage URL. Required.")
	fs.StringVar(&includeTags, "include-tags", "", "CSS selectors to include. Optional. Single selector, comma-separated string, or JSON string array.")
	fs.StringVar(&excludeTags, "exclude-tags", "", "Additional CSS selectors to exclude. Optional. Single selector, comma-separated string, or JSON string array.")
	fs.BoolVar(&emptyTags, "empty-tags", false, "Clear the built-in exclude selector list while keeping user-provided --exclude-tags.")
	fs.BoolVar(&scroll, "scroll", false, "Enable wait and scroll actions before scraping.")
	fs.BoolVar(&skipTLS, "skip-tls", false, "Skip TLS certificate verification for the upstream scrape target. Optional. Default is false.")
	fs.StringVar(&headersRaw, "headers", "", `Root-level request headers as a JSON object, for example {"Authorization":"Bearer token","X-Trace-Id":"abc123"}.`)
	fs.StringVar(&headersFile, "headers-file", "", "Path to a headers file. Supports JSON headers/cookies, HTTP header string, Netscape cookies, or Cookie header value.")
	addProxyFlag(fs, &proxy)
	fs.IntVar(&timeoutSecs, "timeout", defaultTimeoutSecs, "Request timeout in seconds. Optional. Must be > 0. Default is 120.")
	fs.Usage = func() { printScrapeUsage(stderr) }
	if err := fs.Parse(args); err != nil {
		return cliError{code: 2}
	}
	if strings.TrimSpace(output) == "" {
		fs.Usage()
		return cliError{message: "--output is required", code: 2}
	}
	if strings.TrimSpace(targetURL) == "" {
		fs.Usage()
		return cliError{message: "--url is required", code: 2}
	}
	if fs.NArg() > 0 {
		return cliError{message: fmt.Sprintf("unexpected positional arguments: %s", strings.Join(fs.Args(), " ")), code: 2}
	}
	if err := validateTimeoutSecs(timeoutSecs); err != nil {
		return cliError{message: err.Error(), code: 2}
	}
	if err := validateProxyOption(proxy); err != nil {
		return cliError{message: err.Error(), code: 2}
	}
	fileHeaders, err := parseHeadersFile(headersFile)
	if err != nil {
		return cliError{message: err.Error(), code: 2}
	}
	headers, err := parseHeaders(headersRaw)
	if err != nil {
		return cliError{message: err.Error(), code: 2}
	}
	headers = mergeHeaders(fileHeaders, headers)
	include, err := parseSelectorList(includeTags)
	if err != nil {
		return cliError{message: "--include-tags " + err.Error(), code: 2}
	}
	exclude, err := parseSelectorList(excludeTags)
	if err != nil {
		return cliError{message: "--exclude-tags " + err.Error(), code: 2}
	}
	if err := ensureOutputDir(outputDir); err != nil {
		return cliError{message: err.Error(), code: 1}
	}

	payload := buildScrapePayload(targetURL, include, exclude, emptyTags, headers, timeoutSecs, skipTLS, scroll)
	raw, err := firecrawlPost("scrape", payload, timeoutSecs, proxy)
	if err != nil {
		fmt.Fprintln(stdout, "false")
		fmt.Fprintln(stdout, err.Error())
		return cliError{code: 1}
	}
	if hasEmptyScrapeMarkdown(raw) && upstreamSuccessNotFalse(raw) {
		fallbackPayload := clonePayload(payload)
		delete(fallbackPayload, "includeTags")
		delete(fallbackPayload, "excludeTags")
		if fallbackRaw, fallbackErr := firecrawlPost("scrape", fallbackPayload, timeoutSecs, proxy); fallbackErr == nil && upstreamSuccessNotFalse(fallbackRaw) && scrapeData(fallbackRaw) != nil {
			raw = fallbackRaw
		}
	}
	if success, ok := raw["success"].(bool); ok && !success {
		fmt.Fprintln(stdout, "false")
		fmt.Fprintln(stdout, scrapeErrorReason(raw))
		return cliError{code: 1}
	}
	data, ok := raw["data"].(map[string]any)
	if !ok {
		fmt.Fprintln(stdout, "false")
		fmt.Fprintln(stdout, "scrape request failed, upstream response is missing data object")
		return cliError{code: 1}
	}

	result := transformScrapeResult(raw, data, targetURL)

	path := outputPath(output, outputDir)
	if err := os.WriteFile(path, []byte(renderMarkdownFile(result)), 0o644); err != nil {
		fmt.Fprintln(stdout, "false")
		fmt.Fprintln(stdout, err.Error())
		return cliError{code: 1}
	}
	fmt.Fprintln(stdout, "true")
	return nil
}

func runParse(args []string, stdout io.Writer, stderr io.Writer) error {
	fs := flag.NewFlagSet("parse", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var output string
	var outputDir string
	var targetURL string
	var filePath string
	var skipTLS bool
	var proxy string
	timeoutSecs := defaultTimeoutSecs
	fs.StringVar(&output, "output", "", "Export name. Required. The result is saved as <output>.md.")
	fs.StringVar(&outputDir, "path", "", "Directory where the markdown export is saved. Optional. Supports absolute and relative paths. Default is the current directory.")
	fs.StringVar(&targetURL, "url", "", "Target document URL. Required unless --file is provided.")
	fs.StringVar(&filePath, "file", "", "Local document file. Required unless --url is provided.")
	fs.BoolVar(&skipTLS, "skip-tls", false, "Skip TLS certificate verification for URL parsing. Optional. Default is false.")
	addProxyFlag(fs, &proxy)
	fs.IntVar(&timeoutSecs, "timeout", defaultTimeoutSecs, "Request timeout in seconds. Optional. Must be > 0. Default is 120.")
	fs.Usage = func() { printParseUsage(stderr) }
	if err := fs.Parse(args); err != nil {
		return cliError{code: 2}
	}
	if strings.TrimSpace(output) == "" {
		fs.Usage()
		return cliError{message: "--output is required", code: 2}
	}
	targetURL = strings.TrimSpace(targetURL)
	filePath = strings.TrimSpace(filePath)
	hasURL := targetURL != ""
	hasFile := filePath != ""
	if hasURL == hasFile {
		fs.Usage()
		if hasURL {
			return cliError{message: "only one of --url or --file may be provided", code: 2}
		}
		return cliError{message: "one of --url or --file is required", code: 2}
	}
	if fs.NArg() > 0 {
		return cliError{message: fmt.Sprintf("unexpected positional arguments: %s", strings.Join(fs.Args(), " ")), code: 2}
	}
	if err := validateTimeoutSecs(timeoutSecs); err != nil {
		return cliError{message: err.Error(), code: 2}
	}
	if err := validateProxyOption(proxy); err != nil {
		return cliError{message: err.Error(), code: 2}
	}
	if hasFile {
		if err := validateParseFile(filePath); err != nil {
			return cliError{message: err.Error(), code: 2}
		}
	}
	if err := ensureOutputDir(outputDir); err != nil {
		return cliError{message: err.Error(), code: 1}
	}

	var raw map[string]any
	var err error
	if hasFile {
		raw, err = firecrawlPostMultipartFile("parse", filePath, buildParseFileOptions(timeoutSecs), timeoutSecs, proxy)
	} else {
		raw, err = firecrawlPost("scrape", buildParseURLPayload(targetURL, timeoutSecs, skipTLS), timeoutSecs, proxy)
	}
	if err != nil {
		fmt.Fprintln(stdout, "false")
		fmt.Fprintln(stdout, err.Error())
		return cliError{code: 1}
	}
	if success, ok := raw["success"].(bool); ok && !success {
		fmt.Fprintln(stdout, "false")
		fmt.Fprintln(stdout, scrapeErrorReason(raw))
		return cliError{code: 1}
	}
	data, ok := raw["data"].(map[string]any)
	if !ok {
		fmt.Fprintln(stdout, "false")
		fmt.Fprintln(stdout, "parse request failed, upstream response is missing data object")
		return cliError{code: 1}
	}

	result := transformParseResult(raw, data, targetURL)
	path := outputPath(output, outputDir)
	if err := os.WriteFile(path, []byte(renderParseMarkdownFile(result)), 0o644); err != nil {
		fmt.Fprintln(stdout, "false")
		fmt.Fprintln(stdout, err.Error())
		return cliError{code: 1}
	}
	fmt.Fprintln(stdout, "true")
	return nil
}

func runAudioScrape(args []string, stdout io.Writer, stderr io.Writer) error {
	return runAVScrape("audio-scrape", "audio", args, stdout, stderr)
}

func runVideoScrape(args []string, stdout io.Writer, stderr io.Writer) error {
	return runAVScrape("video-scrape", "video", args, stdout, stderr)
}

func runAVScrape(commandName string, format string, args []string, stdout io.Writer, stderr io.Writer) error {
	fs := flag.NewFlagSet(commandName, flag.ContinueOnError)
	fs.SetOutput(stderr)
	var targetURL string
	var proxy string
	timeoutSecs := defaultTimeoutSecs
	fs.StringVar(&targetURL, "url", "", "Target audio/video webpage URL. Required.")
	addProxyFlag(fs, &proxy)
	fs.IntVar(&timeoutSecs, "timeout", defaultTimeoutSecs, "Request timeout in seconds. Optional. Must be > 0. Default is 120.")
	fs.Usage = func() { printAVScrapeUsage(stderr, commandName, format) }
	if err := fs.Parse(args); err != nil {
		return cliError{code: 2}
	}
	if strings.TrimSpace(targetURL) == "" {
		fs.Usage()
		return cliError{message: "--url is required", code: 2}
	}
	if fs.NArg() > 0 {
		return cliError{message: fmt.Sprintf("unexpected positional arguments: %s", strings.Join(fs.Args(), " ")), code: 2}
	}
	if err := validateTimeoutSecs(timeoutSecs); err != nil {
		return cliError{message: err.Error(), code: 2}
	}
	if err := validateProxyOption(proxy); err != nil {
		return cliError{message: err.Error(), code: 2}
	}

	payload := buildAVScrapePayload(targetURL, format, timeoutSecs)
	raw, err := firecrawlPost("scrape", payload, timeoutSecs, proxy)
	if err != nil {
		out := compactJSON(map[string]any{
			"success": false,
			"error":   true,
			"message": err.Error(),
		})
		fmt.Fprintln(stdout, out)
		return cliError{code: 1}
	}
	if success, ok := raw["success"].(bool); ok && !success {
		out := compactJSON(map[string]any{
			"success":  false,
			"error":    true,
			"message":  commandName + " request failed, upstream returned success=false",
			"upstream": raw,
		})
		fmt.Fprintln(stdout, out)
		return cliError{code: 1}
	}
	data, ok := raw["data"].(map[string]any)
	if !ok {
		out := compactJSON(map[string]any{
			"success":  false,
			"error":    true,
			"message":  commandName + " request failed, upstream response is missing data object",
			"upstream": raw,
		})
		fmt.Fprintln(stdout, out)
		return cliError{code: 1}
	}

	fmt.Fprintln(stdout, compactJSON(transformAVScrapeResult(raw, data, format)))
	return nil
}

func runCreditUsage(args []string, stdout io.Writer, stderr io.Writer) error {
	fs := flag.NewFlagSet("credit-usage", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var jsonOutput bool
	var pretty bool
	var proxy string
	fs.BoolVar(&jsonOutput, "json", false, "Output JSON. Optional. JSON is the default output format.")
	fs.BoolVar(&pretty, "pretty", false, "Pretty-print JSON output. Optional.")
	addProxyFlag(fs, &proxy)
	fs.Usage = func() { printCreditUsageUsage(stderr) }
	if err := fs.Parse(args); err != nil {
		return cliError{code: 2}
	}
	if fs.NArg() > 0 {
		return cliError{message: fmt.Sprintf("unexpected positional arguments: %s", strings.Join(fs.Args(), " ")), code: 2}
	}
	if err := validateProxyOption(proxy); err != nil {
		return cliError{message: err.Error(), code: 2}
	}
	_ = jsonOutput

	raw, err := firecrawlGetWithRetry("credit-usage", proxy)
	if err != nil {
		fmt.Fprintln(stdout, formatJSON(map[string]any{
			"success": false,
			"error":   true,
			"message": err.Error(),
		}, pretty))
		return cliError{code: 1}
	}
	fmt.Fprintln(stdout, formatJSON(raw, pretty))
	return nil
}

func buildSearchPayload(query string, country string, limit int, sourceNames []string, timeoutSecs int) map[string]any {
	countryCode := getCountryCodeAlpha2(country)
	sources := make([]map[string]string, 0, len(sourceNames))
	for _, source := range sourceNames {
		sources = append(sources, map[string]string{"type": source})
	}
	timeoutMillis := timeoutMilliseconds(timeoutSecs)
	return map[string]any{
		"query":             query,
		"limit":             limit,
		"sources":           sources,
		"country":           countryCode,
		"timeout":           timeoutMillis,
		"ignoreInvalidURLs": false,
		"scrapeOptions": map[string]any{
			"formats":             []string{},
			"onlyMainContent":     true,
			"maxAge":              172800000,
			"waitFor":             0,
			"mobile":              false,
			"skipTlsVerification": false,
			"timeout":             timeoutMillis,
			"parsers":             []string{},
			"location": map[string]string{
				"country": countryCode,
			},
			"removeBase64Images": true,
			"blockAds":           true,
			"proxy":              "auto",
			"storeInCache":       true,
		},
	}
}

func buildScholarQuery(query string, searchNum int, categories string, timeFrom string, timeTo string) url.Values {
	values := url.Values{}
	values.Set("query", strings.TrimSpace(query))
	values.Set("k", strconv.Itoa(searchNum))
	if strings.TrimSpace(categories) != "" {
		values.Set("categories", strings.TrimSpace(categories))
	}
	if strings.TrimSpace(timeFrom) != "" {
		values.Set("from", strings.TrimSpace(timeFrom))
	}
	if strings.TrimSpace(timeTo) != "" {
		values.Set("to", strings.TrimSpace(timeTo))
	}
	return values
}

func buildTimeoutPayload(timeoutSecs int) map[string]any {
	return map[string]any{
		"timeout": timeoutMilliseconds(timeoutSecs),
	}
}

func buildScrapePayload(targetURL string, includeTags []string, excludeTags []string, emptyTags bool, headers map[string]string, timeoutSecs int, skipTLS bool, scroll bool) map[string]any {
	baseExcludeTags := defaultScrapeExcludeTags()
	if emptyTags {
		baseExcludeTags = nil
	}
	resolvedExclude := stableUnique(append(baseExcludeTags, excludeTags...))
	timeoutMillis := timeoutMilliseconds(timeoutSecs)
	payload := map[string]any{
		"url":                 targetURL,
		"formats":             []string{"markdown"},
		"onlyMainContent":     true,
		"excludeTags":         resolvedExclude,
		"maxAge":              172800000,
		"waitFor":             0,
		"mobile":              false,
		"skipTlsVerification": skipTLS,
		"timeout":             timeoutMillis,
		"parsers":             []string{"pdf"},
		"removeBase64Images":  true,
		"blockAds":            true,
		"proxy":               "auto",
		"storeInCache":        true,
	}
	if scroll {
		payload["actions"] = []map[string]any{
			{
				"type":         "wait",
				"milliseconds": 2,
			},
			{
				"type":      "scroll",
				"direction": "down",
				"selector":  "body",
			},
		}
	}
	if includeTags != nil {
		payload["includeTags"] = includeTags
	}
	if len(headers) > 0 {
		payload["headers"] = headers
	}
	return payload
}

func buildParseURLPayload(targetURL string, timeoutSecs int, skipTLS bool) map[string]any {
	return map[string]any{
		"url":                 targetURL,
		"formats":             []string{"markdown"},
		"parsers":             []string{"pdf"},
		"removeBase64Images":  false,
		"skipTlsVerification": skipTLS,
		"timeout":             timeoutMilliseconds(timeoutSecs),
	}
}

func buildParseFileOptions(timeoutSecs int) map[string]any {
	return map[string]any{
		"formats":            []string{"markdown"},
		"parsers":            []string{"pdf"},
		"removeBase64Images": false,
		"timeout":            timeoutMilliseconds(timeoutSecs),
	}
}

func buildAVScrapePayload(targetURL string, format string, timeoutSecs int) map[string]any {
	return map[string]any{
		"url":     targetURL,
		"formats": []string{format},
		"timeout": timeoutMilliseconds(timeoutSecs),
	}
}

func parseAPIKeys(value string) []string {
	parts := strings.Split(value, ",")
	keys := make([]string, 0, len(parts))
	for _, part := range parts {
		key := strings.TrimSpace(part)
		if key != "" {
			keys = append(keys, key)
		}
	}
	return keys
}

func selectAPIKey(keys []string) string {
	if len(keys) == 0 {
		return ""
	}
	return keys[rand.Intn(len(keys))]
}

func apiKeyFromEnv() (string, error) {
	key := selectAPIKey(parseAPIKeys(os.Getenv(apiKeyEnv)))
	if key == "" {
		return "", fmt.Errorf("%s is required", apiKeyEnv)
	}
	return key, nil
}

func firecrawlGet(endpointName string, proxyRaw string) (map[string]any, error) {
	key, err := apiKeyFromEnv()
	if err != nil {
		return nil, err
	}
	client, err := clientWithProxy(proxyRaw)
	if err != nil {
		return nil, err
	}
	endpoint := endpointURL(endpointName)
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("User-Agent", "firecrawl_cli/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return nil, firecrawlRequestError{endpoint: endpointName, err: err}
	}
	defer resp.Body.Close()
	return parseFirecrawlResponse(endpointName, resp)
}

func firecrawlGetWithRetry(endpointName string, proxyRaw string) (map[string]any, error) {
	return firecrawlWithRetry(func() (map[string]any, error) {
		return firecrawlGet(endpointName, proxyRaw)
	})
}

func firecrawlGetWithJSONBody(endpointName string, query url.Values, payload map[string]any, timeoutSecs int, proxyRaw string) (map[string]any, error) {
	key, err := apiKeyFromEnv()
	if err != nil {
		return nil, err
	}
	endpoint := endpointURL(endpointName)
	if len(query) > 0 {
		separator := "?"
		if strings.Contains(endpoint, "?") {
			separator = "&"
		}
		endpoint += separator + query.Encode()
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodGet, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "firecrawl_cli/1.0")
	client, err := clientWithTimeout(timeoutSecs, proxyRaw)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, firecrawlRequestError{endpoint: endpointName, err: err}
	}
	defer resp.Body.Close()
	return parseFirecrawlResponse(endpointName, resp)
}

func firecrawlGetWithJSONBodyWithRetry(endpointName string, query url.Values, payload map[string]any, timeoutSecs int, proxyRaw string) (map[string]any, error) {
	return firecrawlWithRetry(func() (map[string]any, error) {
		return firecrawlGetWithJSONBody(endpointName, query, payload, timeoutSecs, proxyRaw)
	})
}

func firecrawlPost(endpointName string, payload map[string]any, timeoutSecs int, proxyRaw string) (map[string]any, error) {
	key, err := apiKeyFromEnv()
	if err != nil {
		return nil, err
	}
	endpoint := endpointURL(endpointName)
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "firecrawl_cli/1.0")
	client, err := clientWithTimeout(timeoutSecs, proxyRaw)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, firecrawlRequestError{endpoint: endpointName, err: err}
	}
	defer resp.Body.Close()
	return parseFirecrawlResponse(endpointName, resp)
}

func firecrawlPostMultipartFile(endpointName string, filePath string, options map[string]any, timeoutSecs int, proxyRaw string) (map[string]any, error) {
	key, err := apiKeyFromEnv()
	if err != nil {
		return nil, err
	}
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filepath.Base(filePath))
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(part, file); err != nil {
		return nil, err
	}
	optionsJSON, err := json.Marshal(options)
	if err != nil {
		return nil, err
	}
	if err := writer.WriteField("options", string(optionsJSON)); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}

	endpoint := endpointURL(endpointName)
	req, err := http.NewRequest(http.MethodPost, endpoint, &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("User-Agent", "firecrawl_cli/1.0")
	client, err := clientWithTimeout(timeoutSecs, proxyRaw)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, firecrawlRequestError{endpoint: endpointName, err: err}
	}
	defer resp.Body.Close()
	return parseFirecrawlResponse(endpointName, resp)
}

func firecrawlPostWithRetry(endpointName string, payload map[string]any, timeoutSecs int, proxyRaw string) (map[string]any, error) {
	return firecrawlWithRetry(func() (map[string]any, error) {
		return firecrawlPost(endpointName, payload, timeoutSecs, proxyRaw)
	})
}

func firecrawlWithRetry(call func() (map[string]any, error)) (map[string]any, error) {
	retries := retryCountFromEnv()
	delay := retryDelayFromEnv()
	var lastErr error
	for attempt := 0; attempt <= retries; attempt++ {
		raw, err := call()
		if err == nil {
			return raw, nil
		}
		lastErr = err
		if attempt == retries || !isRetryableFirecrawlError(err) {
			break
		}
		time.Sleep(delay * time.Duration(1<<attempt))
	}
	return nil, lastErr
}

func endpointURL(endpointName string) string {
	baseURL := strings.TrimSpace(os.Getenv(baseURLEnv))
	if baseURL == "" {
		return endpoints[endpointName]
	}
	switch endpointName {
	case "search":
		return joinEndpoint(baseURL, "search")
	case "scholar":
		return joinEndpoint(baseURL, "search/research/papers")
	case "scrape":
		return joinEndpoint(baseURL, "scrape")
	case "parse":
		return joinEndpoint(baseURL, "parse")
	case "credit-usage":
		return joinEndpoint(baseURL, "team/credit-usage")
	default:
		return endpoints[endpointName]
	}
}

func joinEndpoint(baseURL string, path string) string {
	return strings.TrimRight(strings.TrimSpace(baseURL), "/") + "/" + strings.TrimLeft(path, "/")
}

func validateTimeoutSecs(timeoutSecs int) error {
	if timeoutSecs <= 0 {
		return fmt.Errorf("--timeout must be an integer greater than 0")
	}
	if int64(timeoutSecs) > maxTimeoutSecs {
		return fmt.Errorf("--timeout is too large")
	}
	return nil
}

func addProxyFlag(fs *flag.FlagSet, target *string) {
	fs.StringVar(target, "proxy", "", proxyFlagHelp)
}

func validateProxyOption(proxyRaw string) error {
	_, err := parseProxyURL(proxyRaw)
	return err
}

func flagProvided(fs *flag.FlagSet, name string) bool {
	found := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}

func timeoutDuration(timeoutSecs int) time.Duration {
	return time.Duration(timeoutSecs) * time.Second
}

func timeoutMilliseconds(timeoutSecs int) int64 {
	return int64(timeoutSecs) * 1000
}

func validateParseFile(filePath string) error {
	ext := strings.ToLower(filepath.Ext(filePath))
	if !supportedParseFileExt(ext) {
		return fmt.Errorf("--file extension must be one of: .html, .htm, .pdf, .docx, .doc, .odt, .rtf, .xlsx, .xls")
	}
	info, err := os.Stat(filePath)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("--file must be a regular file")
	}
	return nil
}

func supportedParseFileExt(ext string) bool {
	switch ext {
	case ".html", ".htm", ".pdf", ".docx", ".doc", ".odt", ".rtf", ".xlsx", ".xls":
		return true
	default:
		return false
	}
}

func clientWithProxy(proxyRaw string) (*http.Client, error) {
	client := *httpClient
	if strings.TrimSpace(proxyRaw) == "" {
		return &client, nil
	}
	transport, err := transportWithProxy(httpClient.Transport, proxyRaw)
	if err != nil {
		return nil, err
	}
	client.Transport = transport
	return &client, nil
}

func clientWithTimeout(timeoutSecs int, proxyRaw string) (*http.Client, error) {
	client, err := clientWithProxy(proxyRaw)
	if err != nil {
		return nil, err
	}
	client.Timeout = timeoutDuration(timeoutSecs)
	return client, nil
}

func transportWithProxy(base http.RoundTripper, proxyRaw string) (http.RoundTripper, error) {
	proxyURL, err := parseProxyURL(proxyRaw)
	if err != nil {
		return nil, err
	}
	transport := cloneHTTPTransport(base)
	switch strings.ToLower(proxyURL.Scheme) {
	case "http", "https":
		transport.Proxy = http.ProxyURL(proxyURL)
	case "socks4", "socks4a":
		transport.Proxy = nil
		transport.DialContext = socks4DialContext(proxyURL)
	case "socks5", "socks5h":
		transport.Proxy = nil
		transport.DialContext = socks5DialContext(proxyURL)
	default:
		return nil, unsupportedProxySchemeError()
	}
	return transport, nil
}

func cloneHTTPTransport(base http.RoundTripper) *http.Transport {
	if transport, ok := base.(*http.Transport); ok && transport != nil {
		return transport.Clone()
	}
	if transport, ok := http.DefaultTransport.(*http.Transport); ok {
		return transport.Clone()
	}
	return &http.Transport{}
}

func parseProxyURL(proxyRaw string) (*url.URL, error) {
	proxyRaw = strings.TrimSpace(proxyRaw)
	if proxyRaw == "" {
		return nil, nil
	}
	proxyURL, err := url.Parse(proxyRaw)
	if err != nil {
		return nil, fmt.Errorf("--proxy must be a valid proxy URL")
	}
	proxyURL.Scheme = strings.ToLower(proxyURL.Scheme)
	if proxyURL.Scheme == "" || proxyURL.Host == "" || proxyURL.Hostname() == "" {
		return nil, fmt.Errorf("--proxy must include a scheme and host, for example http://127.0.0.1:8080")
	}
	switch proxyURL.Scheme {
	case "http", "https", "socks4", "socks4a", "socks5", "socks5h":
		return proxyURL, nil
	default:
		return nil, unsupportedProxySchemeError()
	}
}

func unsupportedProxySchemeError() error {
	return fmt.Errorf("--proxy scheme must be one of: http, https, socks4, socks4a, socks5, socks5h")
}

func proxyAddress(proxyURL *url.URL) string {
	port := proxyURL.Port()
	if port == "" {
		switch strings.ToLower(proxyURL.Scheme) {
		case "http":
			port = "80"
		case "https":
			port = "443"
		default:
			port = "1080"
		}
	}
	return net.JoinHostPort(proxyURL.Hostname(), port)
}

func socks5DialContext(proxyURL *url.URL) func(context.Context, string, string) (net.Conn, error) {
	var dialer net.Dialer
	return func(ctx context.Context, network string, address string) (net.Conn, error) {
		conn, err := dialer.DialContext(ctx, network, proxyAddress(proxyURL))
		if err != nil {
			return nil, err
		}
		if err := applyContextDeadline(ctx, conn); err != nil {
			conn.Close()
			return nil, err
		}
		if err := socks5Handshake(conn, address, proxyURL); err != nil {
			conn.Close()
			return nil, err
		}
		_ = conn.SetDeadline(time.Time{})
		return conn, nil
	}
}

func socks5Handshake(conn net.Conn, targetAddress string, proxyURL *url.URL) error {
	methods := []byte{0x00}
	if proxyURL.User != nil {
		methods = append(methods, 0x02)
	}
	if _, err := conn.Write(append([]byte{0x05, byte(len(methods))}, methods...)); err != nil {
		return err
	}
	response := make([]byte, 2)
	if _, err := io.ReadFull(conn, response); err != nil {
		return err
	}
	if response[0] != 0x05 {
		return fmt.Errorf("socks5 proxy returned invalid version")
	}
	switch response[1] {
	case 0x00:
	case 0x02:
		if err := socks5UsernamePasswordAuth(conn, proxyURL); err != nil {
			return err
		}
	case 0xff:
		return fmt.Errorf("socks5 proxy requires an unsupported authentication method")
	default:
		return fmt.Errorf("socks5 proxy selected unsupported authentication method")
	}

	host, portRaw, err := net.SplitHostPort(targetAddress)
	if err != nil {
		return err
	}
	port, err := strconv.Atoi(portRaw)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("target address has invalid port")
	}
	request, err := socks5ConnectRequest(host, port)
	if err != nil {
		return err
	}
	if _, err := conn.Write(request); err != nil {
		return err
	}
	header := make([]byte, 4)
	if _, err := io.ReadFull(conn, header); err != nil {
		return err
	}
	if header[0] != 0x05 {
		return fmt.Errorf("socks5 proxy returned invalid response version")
	}
	if header[1] != 0x00 {
		return fmt.Errorf("socks5 proxy connect failed: %s", socks5ReplyMessage(header[1]))
	}
	switch header[3] {
	case 0x01:
		_, err = io.CopyN(io.Discard, conn, 4)
	case 0x03:
		length := []byte{0}
		if _, err = io.ReadFull(conn, length); err != nil {
			return err
		}
		_, err = io.CopyN(io.Discard, conn, int64(length[0]))
	case 0x04:
		_, err = io.CopyN(io.Discard, conn, 16)
	default:
		return fmt.Errorf("socks5 proxy returned invalid address type")
	}
	if err != nil {
		return err
	}
	_, err = io.CopyN(io.Discard, conn, 2)
	return err
}

func socks5UsernamePasswordAuth(conn net.Conn, proxyURL *url.URL) error {
	if proxyURL.User == nil {
		return fmt.Errorf("socks5 proxy requested username/password authentication but --proxy has no credentials")
	}
	username := proxyURL.User.Username()
	password, _ := proxyURL.User.Password()
	if len(username) > 255 || len(password) > 255 {
		return fmt.Errorf("socks5 username and password must be at most 255 bytes")
	}
	request := []byte{0x01, byte(len(username))}
	request = append(request, []byte(username)...)
	request = append(request, byte(len(password)))
	request = append(request, []byte(password)...)
	if _, err := conn.Write(request); err != nil {
		return err
	}
	response := make([]byte, 2)
	if _, err := io.ReadFull(conn, response); err != nil {
		return err
	}
	if response[0] != 0x01 || response[1] != 0x00 {
		return fmt.Errorf("socks5 username/password authentication failed")
	}
	return nil
}

func socks5ConnectRequest(host string, port int) ([]byte, error) {
	request := []byte{0x05, 0x01, 0x00}
	ip := net.ParseIP(host)
	ip4 := ip.To4()
	ip16 := ip.To16()
	switch {
	case ip4 != nil:
		request = append(request, 0x01)
		request = append(request, ip4...)
	case ip16 != nil:
		request = append(request, 0x04)
		request = append(request, ip16...)
	default:
		host = strings.TrimSuffix(host, ".")
		if len(host) == 0 || len(host) > 255 {
			return nil, fmt.Errorf("target hostname must be 1-255 bytes for socks5")
		}
		request = append(request, 0x03, byte(len(host)))
		request = append(request, []byte(host)...)
	}
	portBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(portBytes, uint16(port))
	return append(request, portBytes...), nil
}

func socks5ReplyMessage(code byte) string {
	switch code {
	case 0x01:
		return "general failure"
	case 0x02:
		return "connection not allowed"
	case 0x03:
		return "network unreachable"
	case 0x04:
		return "host unreachable"
	case 0x05:
		return "connection refused"
	case 0x06:
		return "ttl expired"
	case 0x07:
		return "command not supported"
	case 0x08:
		return "address type not supported"
	default:
		return "unknown error"
	}
}

func socks4DialContext(proxyURL *url.URL) func(context.Context, string, string) (net.Conn, error) {
	var dialer net.Dialer
	return func(ctx context.Context, network string, address string) (net.Conn, error) {
		conn, err := dialer.DialContext(ctx, network, proxyAddress(proxyURL))
		if err != nil {
			return nil, err
		}
		if err := applyContextDeadline(ctx, conn); err != nil {
			conn.Close()
			return nil, err
		}
		if err := socks4Handshake(ctx, conn, address, proxyURL); err != nil {
			conn.Close()
			return nil, err
		}
		_ = conn.SetDeadline(time.Time{})
		return conn, nil
	}
}

func socks4Handshake(ctx context.Context, conn net.Conn, targetAddress string, proxyURL *url.URL) error {
	host, portRaw, err := net.SplitHostPort(targetAddress)
	if err != nil {
		return err
	}
	port, err := strconv.Atoi(portRaw)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("target address has invalid port")
	}
	ip := net.ParseIP(host).To4()
	useSocks4A := strings.EqualFold(proxyURL.Scheme, "socks4a")
	if ip == nil && !useSocks4A {
		ips, err := net.DefaultResolver.LookupIP(ctx, "ip4", host)
		if err != nil {
			return err
		}
		for _, resolved := range ips {
			if ip = resolved.To4(); ip != nil {
				break
			}
		}
		if ip == nil {
			return fmt.Errorf("socks4 requires an IPv4 target")
		}
	}
	if ip == nil {
		ip = net.IPv4(0, 0, 0, 1).To4()
	}

	userID := ""
	if proxyURL.User != nil {
		userID = proxyURL.User.Username()
		if password, ok := proxyURL.User.Password(); ok && password != "" {
			userID += ":" + password
		}
	}
	request := []byte{0x04, 0x01}
	portBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(portBytes, uint16(port))
	request = append(request, portBytes...)
	request = append(request, ip...)
	request = append(request, []byte(userID)...)
	request = append(request, 0x00)
	if useSocks4A && net.ParseIP(host) == nil {
		request = append(request, []byte(host)...)
		request = append(request, 0x00)
	}
	if _, err := conn.Write(request); err != nil {
		return err
	}
	response := make([]byte, 8)
	if _, err := io.ReadFull(conn, response); err != nil {
		return err
	}
	if response[0] != 0x00 {
		return fmt.Errorf("socks4 proxy returned invalid response")
	}
	if response[1] != 0x5a {
		return fmt.Errorf("socks4 proxy connect failed: %s", socks4ReplyMessage(response[1]))
	}
	return nil
}

func socks4ReplyMessage(code byte) string {
	switch code {
	case 0x5b:
		return "request rejected or failed"
	case 0x5c:
		return "client is not running identd"
	case 0x5d:
		return "client identd user ID mismatch"
	default:
		return "unknown error"
	}
}

func applyContextDeadline(ctx context.Context, conn net.Conn) error {
	if deadline, ok := ctx.Deadline(); ok {
		return conn.SetDeadline(deadline)
	}
	return nil
}

func parseFirecrawlResponse(endpointName string, resp *http.Response) (map[string]any, error) {
	respBody, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, readErr
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var parsed map[string]any
		if len(respBody) > 0 {
			_ = json.Unmarshal(respBody, &parsed)
		}
		if parsed != nil {
			return nil, firecrawlHTTPError{endpoint: endpointName, statusCode: resp.StatusCode, message: scrapeErrorReason(parsed)}
		}
		return nil, firecrawlHTTPError{endpoint: endpointName, statusCode: resp.StatusCode}
	}
	var parsed map[string]any
	if len(respBody) > 0 {
		if err := json.Unmarshal(respBody, &parsed); err != nil {
			return nil, fmt.Errorf("%s response JSON parse failed: %w", endpointName, err)
		}
	}
	if parsed == nil {
		return nil, fmt.Errorf("%s response is empty", endpointName)
	}
	return parsed, nil
}

type firecrawlHTTPError struct {
	endpoint   string
	statusCode int
	message    string
}

func (e firecrawlHTTPError) Error() string {
	if e.message != "" {
		return fmt.Sprintf("%s HTTP status error: %d: %s", e.endpoint, e.statusCode, e.message)
	}
	return fmt.Sprintf("%s HTTP status error: %d", e.endpoint, e.statusCode)
}

func isRetryableFirecrawlError(err error) bool {
	var httpErr firecrawlHTTPError
	if errors.As(err, &httpErr) {
		return httpErr.statusCode >= 500 && httpErr.statusCode < 600
	}
	var requestErr firecrawlRequestError
	return errors.As(err, &requestErr)
}

type firecrawlRequestError struct {
	endpoint string
	err      error
}

func (e firecrawlRequestError) Error() string {
	return fmt.Sprintf("%s request error: %v", e.endpoint, e.err)
}

func (e firecrawlRequestError) Unwrap() error {
	return e.err
}

func retryCountFromEnv() int {
	raw := strings.TrimSpace(os.Getenv("FIRECRAWL_RETRY_COUNT"))
	if raw == "" {
		return defaultRetryCount
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return defaultRetryCount
	}
	return value
}

func retryDelayFromEnv() time.Duration {
	raw := strings.TrimSpace(os.Getenv("FIRECRAWL_RETRY_BASE_DELAY"))
	if raw == "" {
		return defaultRetryDelay
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || value < 0 {
		return defaultRetryDelay
	}
	return time.Duration(value * float64(time.Second))
}

func transformSearchResult(raw map[string]any, data map[string]any) map[string]any {
	return map[string]any{
		"success": raw["success"],
		"data": map[string]any{
			"web":    mapItems(data["web"], []string{"title", "description", "url"}),
			"news":   mapItems(data["news"], []string{"title", "snippet", "url", "date"}),
			"images": mapItems(data["images"], []string{"title", "imageUrl", "url"}),
		},
		"creditsUsed": valueOrNil(raw, "creditsUsed"),
	}
}

func transformScholarResult(raw map[string]any) map[string]any {
	return map[string]any{
		"success": raw["success"],
		"data": map[string]any{
			"scholar": mapItems(raw["results"], []string{"title", "abstract", "paperId", "primaryId", "score"}),
		},
	}
}

func transformScrapeResult(raw map[string]any, data map[string]any, targetURL string) map[string]any {
	metadata, _ := data["metadata"].(map[string]any)
	markdown, _ := data["markdown"].(string)

	return map[string]any{
		"success":     raw["success"],
		"proxyUsed":   valueOrNil(metadata, "proxyUsed"),
		"title":       valueOrNil(metadata, "title"),
		"description": valueOrNil(metadata, "description"),
		"url":         scrapeURL(metadata, targetURL),
		"language":    valueOrNil(metadata, "language"),
		"markdown":    decodeMarkdownContent(markdown),
		"creditsUsed": valueOrNil(metadata, "creditsUsed"),
	}
}

func transformParseResult(raw map[string]any, data map[string]any, targetURL string) map[string]any {
	metadata, _ := data["metadata"].(map[string]any)
	markdown, _ := data["markdown"].(string)
	creditsUsed := valueOrNil(metadata, "creditsUsed")
	if creditsUsed == nil {
		creditsUsed = valueOrNil(raw, "creditsUsed")
	}
	return map[string]any{
		"title":       valueOrNil(metadata, "title"),
		"url":         scrapeURL(metadata, targetURL),
		"language":    valueOrNil(metadata, "language"),
		"creditsUsed": creditsUsed,
		"markdown":    decodeMarkdownContent(markdown),
	}
}

func decodeMarkdownContent(markdown string) string {
	decodedMarkdown := markdown
	if decoded, err := url.PathUnescape(markdown); err == nil {
		decodedMarkdown = decoded
	}
	return strings.ReplaceAll(decodedMarkdown, `\n`, "\n")
}

type audioScrapeResult struct {
	CreditsUsed any `json:"creditsUsed"`
	Title       any `json:"title"`
	Description any `json:"description"`
	Audio       any `json:"audio"`
	Success     any `json:"success"`
}

type videoScrapeResult struct {
	CreditsUsed any `json:"creditsUsed"`
	Title       any `json:"title"`
	Description any `json:"description"`
	Video       any `json:"video"`
	Success     any `json:"success"`
}

func transformAVScrapeResult(raw map[string]any, data map[string]any, format string) any {
	metadata, _ := data["metadata"].(map[string]any)
	creditsUsed := valueOrNil(metadata, "creditsUsed")
	if creditsUsed == nil {
		creditsUsed = valueOrNil(raw, "creditsUsed")
	}
	if format == "video" {
		return videoScrapeResult{
			CreditsUsed: creditsUsed,
			Title:       valueOrNil(metadata, "title"),
			Description: valueOrNil(metadata, "description"),
			Video:       valueOrNil(data, "video"),
			Success:     valueOrNil(raw, "success"),
		}
	}
	return audioScrapeResult{
		CreditsUsed: creditsUsed,
		Title:       valueOrNil(metadata, "title"),
		Description: valueOrNil(metadata, "description"),
		Audio:       valueOrNil(data, "audio"),
		Success:     valueOrNil(raw, "success"),
	}
}

func scrapeURL(metadata map[string]any, targetURL string) string {
	for _, key := range []string{"url", "sourceURL", "ogUrl"} {
		if val, ok := valueOrNil(metadata, key).(string); ok && strings.TrimSpace(val) != "" {
			return val
		}
	}
	return targetURL
}

func scrapeData(raw map[string]any) map[string]any {
	data, _ := raw["data"].(map[string]any)
	return data
}

func hasEmptyScrapeMarkdown(raw map[string]any) bool {
	data := scrapeData(raw)
	if data == nil {
		return false
	}
	markdown, ok := data["markdown"].(string)
	return ok && markdown == ""
}

func upstreamSuccessNotFalse(raw map[string]any) bool {
	success, ok := raw["success"].(bool)
	return !ok || success
}

func clonePayload(payload map[string]any) map[string]any {
	clone := make(map[string]any, len(payload))
	for key, value := range payload {
		clone[key] = value
	}
	return clone
}

func mapItems(items any, fields []string) []map[string]any {
	rawItems, ok := items.([]any)
	if !ok {
		return []map[string]any{}
	}
	mapped := make([]map[string]any, 0, len(rawItems))
	for _, item := range rawItems {
		row := make(map[string]any, len(fields))
		obj, _ := item.(map[string]any)
		for _, field := range fields {
			if obj == nil {
				row[field] = nil
				continue
			}
			row[field] = valueOrNil(obj, field)
		}
		mapped = append(mapped, row)
	}
	return mapped
}

func valueOrNil(obj map[string]any, key string) any {
	if obj == nil {
		return nil
	}
	if val, ok := obj[key]; ok {
		return val
	}
	return nil
}

func compactJSON(payload any) string {
	return formatJSON(payload, false)
}

func formatJSON(payload any, pretty bool) string {
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	if pretty {
		encoder.SetIndent("", "  ")
	}
	if err := encoder.Encode(payload); err != nil {
		return `{"success":false,"error":true,"message":"failed to encode JSON"}`
	}
	return strings.TrimSuffix(out.String(), "\n")
}

func renderMarkdownFile(result map[string]any) string {
	return fmt.Sprintf("## title: %s\n## description: %s\n## url: %s\n## language: %s\n## creditsUsed: %s\n\n---\n\n%s\n",
		stringValue(result["title"]),
		stringValue(result["description"]),
		stringValue(result["url"]),
		stringValue(result["language"]),
		stringValue(result["creditsUsed"]),
		stringValue(result["markdown"]),
	)
}

func renderParseMarkdownFile(result map[string]any) string {
	return fmt.Sprintf("## title: %s\n## url: %s\n## language: %s\n## creditsUsed: %s\n\n%s\n",
		stringValue(result["title"]),
		stringValue(result["url"]),
		stringValue(result["language"]),
		stringValue(result["creditsUsed"]),
		stringValue(result["markdown"]),
	)
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	switch v := value.(type) {
	case string:
		return v
	default:
		return fmt.Sprint(v)
	}
}

func outputPath(output string, outputDir string) string {
	name := filepath.Base(strings.TrimSpace(output))
	if !strings.HasSuffix(strings.ToLower(name), ".md") {
		name += ".md"
	}
	dir := strings.TrimSpace(outputDir)
	if dir == "" {
		dir = "."
	}
	return filepath.Join(dir, name)
}

func ensureOutputDir(outputDir string) error {
	dir := strings.TrimSpace(outputDir)
	if dir == "" {
		dir = "."
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create output path %q: %w", dir, err)
	}
	return nil
}

func parseHeaders(raw string) (map[string]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return nil, fmt.Errorf("--headers must be a valid JSON object: %w", err)
	}
	headers := make(map[string]string, len(decoded))
	for key, value := range decoded {
		str, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("--headers values must be strings")
		}
		headers[key] = str
	}
	return headers, nil
}

func parseHeadersFile(path string) (map[string]string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("--headers-file could not be read: %w", err)
	}
	headers, err := parseHeadersFileContent(string(content))
	if err != nil {
		return nil, fmt.Errorf("--headers-file %s", err)
	}
	return headers, nil
}

func parseHeadersFileContent(raw string) (map[string]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if strings.HasPrefix(raw, "{") || strings.HasPrefix(raw, "[") {
		return parseJSONHeadersFile(raw)
	}
	if headers, matched, err := parseNetscapeCookies(raw); matched {
		return headers, err
	}
	if strings.Contains(raw, ":") {
		if headers, err := parseHeaderString(raw); err == nil {
			return headers, nil
		}
	}
	if looksLikeCookieHeaderValue(raw) {
		return map[string]string{"Cookie": normalizeCookieHeaderValue(raw)}, nil
	}
	return nil, fmt.Errorf("must contain JSON headers/cookies, an HTTP header string, a Netscape cookie file, or a Cookie header value")
}

func parseJSONHeadersFile(raw string) (map[string]string, error) {
	var object map[string]any
	if err := json.Unmarshal([]byte(raw), &object); err == nil {
		if headers, ok := stringMapHeaders(object); ok {
			return headers, nil
		}
		if value, ok := object["headers"]; ok {
			headers, err := parseJSONNamedValueHeaders(value)
			if err != nil {
				return nil, err
			}
			return headers, nil
		}
		if value, ok := object["cookies"]; ok {
			headers, err := parseJSONCookieObjects(value)
			if err != nil {
				return nil, err
			}
			return headers, nil
		}
		return nil, fmt.Errorf("JSON object must contain string header values or a headers/cookies array")
	}

	var array []map[string]any
	if err := json.Unmarshal([]byte(raw), &array); err != nil {
		return nil, fmt.Errorf("must contain valid JSON: %w", err)
	}
	if looksLikeCookieObjectArray(array) {
		return cookieHeadersFromObjects(array)
	}
	return namedValueHeadersFromObjects(array)
}

func stringMapHeaders(object map[string]any) (map[string]string, bool) {
	headers := make(map[string]string, len(object))
	for key, value := range object {
		str, ok := value.(string)
		if !ok {
			return nil, false
		}
		headers[key] = str
	}
	return headers, true
}

func parseJSONNamedValueHeaders(value any) (map[string]string, error) {
	items, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("JSON headers field must be an array")
	}
	objects := make([]map[string]any, 0, len(items))
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("JSON headers array items must be objects")
		}
		objects = append(objects, object)
	}
	return namedValueHeadersFromObjects(objects)
}

func parseJSONCookieObjects(value any) (map[string]string, error) {
	items, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("JSON cookies field must be an array")
	}
	objects := make([]map[string]any, 0, len(items))
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("JSON cookies array items must be objects")
		}
		objects = append(objects, object)
	}
	return cookieHeadersFromObjects(objects)
}

func namedValueHeadersFromObjects(objects []map[string]any) (map[string]string, error) {
	headers := map[string]string{}
	for _, object := range objects {
		name, value, ok := objectNameValue(object)
		if !ok {
			return nil, fmt.Errorf("JSON header array items must contain string name and value fields")
		}
		if !validHeaderName(name) {
			return nil, fmt.Errorf("JSON header array contains an invalid header name")
		}
		headers[name] = value
	}
	if len(headers) == 0 {
		return nil, fmt.Errorf("JSON header array contains no headers")
	}
	return headers, nil
}

func cookieHeadersFromObjects(objects []map[string]any) (map[string]string, error) {
	cookies := []string{}
	for _, object := range objects {
		name, value, ok := objectNameValue(object)
		if !ok {
			return nil, fmt.Errorf("JSON cookie array items must contain string name and value fields")
		}
		if strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("JSON cookie array contains a cookie without a name")
		}
		cookies = append(cookies, name+"="+value)
	}
	if len(cookies) == 0 {
		return nil, fmt.Errorf("JSON cookie array contains no cookies")
	}
	return map[string]string{"Cookie": strings.Join(cookies, "; ")}, nil
}

func objectNameValue(object map[string]any) (string, string, bool) {
	name, nameOK := object["name"].(string)
	value, valueOK := object["value"].(string)
	return strings.TrimSpace(name), value, nameOK && valueOK
}

func looksLikeCookieObjectArray(objects []map[string]any) bool {
	if len(objects) == 0 {
		return false
	}
	cookieKeys := []string{"domain", "expirationDate", "hostOnly", "httpOnly", "path", "sameSite", "secure", "session", "storeId"}
	for _, object := range objects {
		for _, key := range cookieKeys {
			if _, ok := object[key]; ok {
				return true
			}
		}
	}
	return false
}

func mergeHeaders(base map[string]string, override map[string]string) map[string]string {
	if len(base) == 0 {
		return override
	}
	if len(override) == 0 {
		return base
	}
	merged := make(map[string]string, len(base)+len(override))
	for key, value := range base {
		merged[key] = value
	}
	for key, value := range override {
		merged[key] = value
	}
	return merged
}

func parseHeaderString(raw string) (map[string]string, error) {
	headers := map[string]string{}
	var lastName string
	for _, line := range strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		if lastName != "" && (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")) {
			headers[lastName] = strings.TrimSpace(headers[lastName] + " " + strings.TrimSpace(line))
			continue
		}
		if isHTTPStartLine(line) {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(line), ":") {
			lastName = ""
			continue
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok || !validHeaderName(strings.TrimSpace(name)) {
			return nil, fmt.Errorf("contains an invalid HTTP header line")
		}
		name = strings.TrimSpace(name)
		value = strings.TrimSpace(value)
		if existing, ok := headers[name]; ok && existing != "" && value != "" {
			headers[name] = existing + ", " + value
		} else if existing, ok := headers[name]; ok && existing != "" {
			headers[name] = existing
		} else {
			headers[name] = value
		}
		lastName = name
	}
	if len(headers) == 0 {
		return nil, fmt.Errorf("contains no HTTP headers")
	}
	return headers, nil
}

func parseNetscapeCookies(raw string) (map[string]string, bool, error) {
	var cookies []string
	matched := false
	for _, line := range strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(strings.TrimRight(line, "\r"))
		if line == "" {
			continue
		}
		if strings.Contains(line, "Netscape HTTP Cookie File") {
			matched = true
			continue
		}
		if strings.HasPrefix(line, "#HttpOnly_") {
			matched = true
			line = strings.TrimPrefix(line, "#HttpOnly_")
		} else if strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.SplitN(line, "\t", 7)
		if len(fields) != 7 {
			if matched {
				return nil, true, fmt.Errorf("contains an invalid Netscape cookie line")
			}
			return nil, false, nil
		}
		matched = true
		name := strings.TrimSpace(fields[5])
		value := strings.TrimSpace(fields[6])
		if name == "" {
			return nil, true, fmt.Errorf("contains a Netscape cookie without a name")
		}
		cookies = append(cookies, name+"="+value)
	}
	if !matched {
		return nil, false, nil
	}
	if len(cookies) == 0 {
		return nil, true, fmt.Errorf("contains no Netscape cookies")
	}
	return map[string]string{"Cookie": strings.Join(cookies, "; ")}, true, nil
}

func looksLikeCookieHeaderValue(raw string) bool {
	raw = normalizeCookieHeaderValue(raw)
	if raw == "" || strings.ContainsAny(raw, "\r\n") {
		return false
	}
	for _, part := range strings.Split(raw, ";") {
		name, _, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok || strings.TrimSpace(name) == "" {
			return false
		}
	}
	return true
}

func normalizeCookieHeaderValue(raw string) string {
	parts := []string{}
	for _, part := range strings.Split(strings.TrimSpace(raw), ";") {
		part = strings.TrimSpace(part)
		if part != "" {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, "; ")
}

func validHeaderName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if r > unicode.MaxASCII || !(unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("!#$%&'*+-.^_`|~", r)) {
			return false
		}
	}
	return true
}

func isHTTPStartLine(line string) bool {
	line = strings.TrimSpace(line)
	if strings.HasPrefix(line, "HTTP/") {
		return true
	}
	methods := []string{"CONNECT ", "DELETE ", "GET ", "HEAD ", "OPTIONS ", "PATCH ", "POST ", "PUT ", "TRACE "}
	for _, method := range methods {
		if strings.HasPrefix(line, method) {
			return true
		}
	}
	return false
}

func parseSelectorList(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if strings.HasPrefix(raw, "[") {
		var items []string
		if err := json.Unmarshal([]byte(raw), &items); err != nil {
			return nil, fmt.Errorf("must be a comma-separated string or JSON string array: %w", err)
		}
		return stableUnique(cleanStrings(items)), nil
	}
	parts := strings.Split(raw, ",")
	return stableUnique(cleanStrings(parts)), nil
}

func cleanStrings(items []string) []string {
	cleaned := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item != "" {
			cleaned = append(cleaned, item)
		}
	}
	return cleaned
}

func stableUnique(items []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(items))
	for _, item := range items {
		if seen[item] {
			continue
		}
		seen[item] = true
		result = append(result, item)
	}
	return result
}

func scrapeErrorReason(raw map[string]any) string {
	for _, key := range []string{"message", "error"} {
		if val, ok := raw[key]; ok && val != nil {
			return stringValue(val)
		}
	}
	if upstream, ok := raw["upstream"].(map[string]any); ok {
		return scrapeErrorReason(upstream)
	}
	return "unknown error"
}

func mapSearchTime(value string) (string, error) {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "":
		return "", nil
	case "hour":
		return "qdr:h", nil
	case "day":
		return "qdr:d", nil
	case "week":
		return "qdr:w", nil
	case "month":
		return "qdr:m", nil
	case "year":
		return "qdr:y", nil
	default:
		return "", fmt.Errorf(`--search-time must be one of "hour", "day", "week", "month", "year"`)
	}
}

func loadCountryAliases() map[string]string {
	data, err := embeddedData.ReadFile("data/country_aliases.json")
	if err != nil {
		return map[string]string{}
	}
	var raw map[string][]string
	if err := json.Unmarshal(data, &raw); err != nil {
		return map[string]string{}
	}
	aliases := map[string]string{}
	for code, names := range raw {
		code = strings.ToUpper(code)
		for _, name := range names {
			for _, variant := range countryVariants(name) {
				key := normalizeCountry(variant)
				if key != "" {
					aliases[key] = code
				}
			}
		}
	}
	return aliases
}

func getCountryCodeAlpha2(country string) string {
	name := strings.TrimSpace(country)
	if name == "" {
		return "US"
	}
	norm := normalizeCountry(name)
	if code, ok := countryAliases[norm]; ok {
		return code
	}
	if len([]rune(name)) == 2 && isLetters(name) {
		return strings.ToUpper(name)
	}
	if code, ok := countryAliases[normalizeCountry(strings.ToUpper(name))]; ok {
		return code
	}
	return "US"
}

func countryVariants(alias string) []string {
	base := strings.TrimSpace(alias)
	if base == "" {
		return nil
	}
	variants := []string{base, normalizeCountry(base), stripCountryPunctuation(normalizeCountry(base))}
	if strings.Contains(base, ",") {
		parts := strings.Split(base, ",")
		cleaned := make([]string, 0, len(parts))
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part != "" {
				cleaned = append(cleaned, part)
			}
		}
		if len(cleaned) >= 2 {
			for i, j := 0, len(cleaned)-1; i < j; i, j = i+1, j-1 {
				cleaned[i], cleaned[j] = cleaned[j], cleaned[i]
			}
			reordered := strings.Join(cleaned, " ")
			variants = append(variants, reordered, normalizeCountry(reordered))
		}
	}
	parts := strings.Fields(normalizeCountry(base))
	if len(parts) == 2 {
		variants = append(variants, parts[1]+" "+parts[0])
	}
	return stableUnique(variants)
}

func normalizeCountry(value string) string {
	value = norm.NFKD.String(value)
	var folded strings.Builder
	for _, r := range value {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		folded.WriteRune(r)
	}
	value = strings.ReplaceAll(folded.String(), "\u3000", " ")
	value = strings.TrimSpace(strings.ToLower(value))
	var b strings.Builder
	previousSpace := false
	for _, r := range value {
		switch {
		case r == '_' || unicode.IsSpace(r):
			if !previousSpace {
				b.WriteRune(' ')
				previousSpace = true
			}
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '\'' || r == '-':
			b.WriteRune(r)
			previousSpace = false
		default:
			if !previousSpace {
				b.WriteRune(' ')
				previousSpace = true
			}
		}
	}
	return strings.TrimSpace(b.String())
}

func stripCountryPunctuation(value string) string {
	re := regexp.MustCompile(`[^\p{L}\p{N}\s]`)
	return strings.TrimSpace(re.ReplaceAllString(value, ""))
}

func isLetters(value string) bool {
	for _, r := range value {
		if !unicode.IsLetter(r) {
			return false
		}
	}
	return true
}

func defaultScrapeExcludeTags() []string {
	return []string{
		"script", "style", "noscript", "form", "input", "button", "select", "textarea",
		"nav", ".nav", ".navbar", ".navigation", ".menu", ".menubar", "header", "footer", "aside",
		`[class*="logo"]`, `[class*="brand"]`, `[id*="logo"]`, `img[alt*="logo"]`, `img[src*="logo"]`,
		`[id*="brand"]`, `[class^="category--"]`, `[id^="category--"]`, `[class$="--category"]`, `[id$="--category"]`,
		`[class^="skip"]`, `[class*="accessib"]`, ".sr-only", ".visually-hidden",
		`[class*="-ad-"]`, `[class*="_ad_"]`, `[class$="-ad"]`, `[class$="_ad"]`, `[class^="ad-"]`,
		`[class^="ad_"]`, `[class*="advert"]`, ".sidebar", "#sidebar", `[class*="sidebar"]`,
		`[class*="sider"]`, `[class^="menu-"]`, `[class^="menu_"]`, `[class$="_module"]`,
		`[class$="-module"]`, `[class*="breadcrumb"]`, `[class*="pagination"]`, `[class*="relate"]`,
		`[class*="recommend"]`, `[class*="trending"]`, `[class^="header-"]`, `[class$="-header"]`,
		`[class^="header_"]`, `[class$="_header"]`, `[class*="footer"]`, `[class$="-offset"]`,
		`[class*="-offset-"]`, `[class$="_offset"]`, `[class*="_offset_"]`,
	}
}

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
