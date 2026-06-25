package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/rand"
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
)

//go:embed data/country_aliases.json
var embeddedData embed.FS

var (
	httpClient = &http.Client{Timeout: 30 * time.Second}
	endpoints  = map[string]string{
		"search":       joinEndpoint(defaultBaseURL, "search"),
		"scrape":       joinEndpoint(defaultBaseURL, "scrape"),
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
	case "scrape":
		return runScrape(args[1:], stdout, stderr)
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
	timeoutSecs := defaultTimeoutSecs
	fs.StringVar(&query, "query", "", "Search keywords. Required.")
	fs.StringVar(&country, "country", "", "Country or region name/ISO code. Optional. Default is US.")
	fs.IntVar(&searchNum, "search-num", 20, "Number of results to return. Optional. Range: 1-100. Default is 20.")
	fs.StringVar(&searchTime, "search-time", "", `Time filter. Optional. One of: "hour", "day", "week", "month", "year".`)
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
	tbs, err := mapSearchTime(searchTime)
	if err != nil {
		return cliError{message: err.Error(), code: 2}
	}

	payload := buildSearchPayload(query, country, searchNum, sources, timeoutSecs)
	if tbs != "" {
		payload["tbs"] = tbs
	}

	raw, err := firecrawlPostWithRetry("search", payload, timeoutSecs)
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

func runScrape(args []string, stdout io.Writer, stderr io.Writer) error {
	fs := flag.NewFlagSet("scrape", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var output string
	var outputDir string
	var targetURL string
	var includeTags string
	var excludeTags string
	var emptyTags bool
	var skipTLS bool
	var headersRaw string
	timeoutSecs := defaultTimeoutSecs
	fs.StringVar(&output, "output", "", "Export name. Required. The result is saved as <output>.md.")
	fs.StringVar(&outputDir, "path", "", "Directory where the markdown export is saved. Optional. Supports absolute and relative paths. Default is the current directory.")
	fs.StringVar(&targetURL, "url", "", "Target webpage URL. Required.")
	fs.StringVar(&includeTags, "include-tags", "", "CSS selectors to include. Optional. Single selector, comma-separated string, or JSON string array.")
	fs.StringVar(&excludeTags, "exclude-tags", "", "Additional CSS selectors to exclude. Optional. Single selector, comma-separated string, or JSON string array.")
	fs.BoolVar(&emptyTags, "empty-tags", false, "Clear the built-in exclude selector list while keeping user-provided --exclude-tags.")
	fs.BoolVar(&skipTLS, "skip-tls", false, "Skip TLS certificate verification for the upstream scrape target. Optional. Default is false.")
	fs.StringVar(&headersRaw, "headers", "", `Root-level request headers as a JSON object, for example {"Authorization":"Bearer token","X-Trace-Id":"abc123"}.`)
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
	headers, err := parseHeaders(headersRaw)
	if err != nil {
		return cliError{message: err.Error(), code: 2}
	}
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

	payload := buildScrapePayload(targetURL, include, exclude, emptyTags, headers, timeoutSecs, skipTLS)
	raw, err := firecrawlPost("scrape", payload, timeoutSecs)
	if err != nil {
		fmt.Fprintln(stdout, "false")
		fmt.Fprintln(stdout, err.Error())
		return cliError{code: 1}
	}
	if hasEmptyScrapeMarkdown(raw) && upstreamSuccessNotFalse(raw) {
		fallbackPayload := clonePayload(payload)
		delete(fallbackPayload, "includeTags")
		delete(fallbackPayload, "excludeTags")
		if fallbackRaw, fallbackErr := firecrawlPost("scrape", fallbackPayload, timeoutSecs); fallbackErr == nil && upstreamSuccessNotFalse(fallbackRaw) && scrapeData(fallbackRaw) != nil {
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

func runCreditUsage(args []string, stdout io.Writer, stderr io.Writer) error {
	fs := flag.NewFlagSet("credit-usage", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var jsonOutput bool
	var pretty bool
	fs.BoolVar(&jsonOutput, "json", false, "Output JSON. Optional. JSON is the default output format.")
	fs.BoolVar(&pretty, "pretty", false, "Pretty-print JSON output. Optional.")
	fs.Usage = func() { printCreditUsageUsage(stderr) }
	if err := fs.Parse(args); err != nil {
		return cliError{code: 2}
	}
	if fs.NArg() > 0 {
		return cliError{message: fmt.Sprintf("unexpected positional arguments: %s", strings.Join(fs.Args(), " ")), code: 2}
	}
	_ = jsonOutput

	raw, err := firecrawlGetWithRetry("credit-usage")
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

func buildScrapePayload(targetURL string, includeTags []string, excludeTags []string, emptyTags bool, headers map[string]string, timeoutSecs int, skipTLS bool) map[string]any {
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
	if includeTags != nil {
		payload["includeTags"] = includeTags
	}
	if len(headers) > 0 {
		payload["headers"] = headers
	}
	return payload
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

func firecrawlGet(endpointName string) (map[string]any, error) {
	key, err := apiKeyFromEnv()
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
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, firecrawlRequestError{endpoint: endpointName, err: err}
	}
	defer resp.Body.Close()
	return parseFirecrawlResponse(endpointName, resp)
}

func firecrawlGetWithRetry(endpointName string) (map[string]any, error) {
	return firecrawlWithRetry(func() (map[string]any, error) {
		return firecrawlGet(endpointName)
	})
}

func firecrawlPost(endpointName string, payload map[string]any, timeoutSecs int) (map[string]any, error) {
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
	client := clientWithTimeout(timeoutSecs)
	resp, err := client.Do(req)
	if err != nil {
		return nil, firecrawlRequestError{endpoint: endpointName, err: err}
	}
	defer resp.Body.Close()
	return parseFirecrawlResponse(endpointName, resp)
}

func firecrawlPostWithRetry(endpointName string, payload map[string]any, timeoutSecs int) (map[string]any, error) {
	return firecrawlWithRetry(func() (map[string]any, error) {
		return firecrawlPost(endpointName, payload, timeoutSecs)
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
	case "scrape":
		return joinEndpoint(baseURL, "scrape")
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

func timeoutDuration(timeoutSecs int) time.Duration {
	return time.Duration(timeoutSecs) * time.Second
}

func timeoutMilliseconds(timeoutSecs int) int64 {
	return int64(timeoutSecs) * 1000
}

func clientWithTimeout(timeoutSecs int) *http.Client {
	client := *httpClient
	client.Timeout = timeoutDuration(timeoutSecs)
	return &client
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

func transformScrapeResult(raw map[string]any, data map[string]any, targetURL string) map[string]any {
	metadata, _ := data["metadata"].(map[string]any)
	markdown, _ := data["markdown"].(string)
	decodedMarkdown := markdown
	if decoded, err := url.PathUnescape(markdown); err == nil {
		decodedMarkdown = decoded
	}
	decodedMarkdown = strings.ReplaceAll(decodedMarkdown, `\n`, "\n")

	return map[string]any{
		"success":     raw["success"],
		"proxyUsed":   valueOrNil(metadata, "proxyUsed"),
		"title":       valueOrNil(metadata, "title"),
		"description": valueOrNil(metadata, "description"),
		"url":         scrapeURL(metadata, targetURL),
		"language":    valueOrNil(metadata, "language"),
		"markdown":    decodedMarkdown,
		"creditsUsed": valueOrNil(metadata, "creditsUsed"),
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
	var (
		out []byte
		err error
	)
	if pretty {
		out, err = json.MarshalIndent(payload, "", "  ")
	} else {
		out, err = json.Marshal(payload)
	}
	if err != nil {
		return `{"success":false,"error":true,"message":"failed to encode JSON"}`
	}
	return string(out)
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
  firecrawl aggregated --query <keywords> [--country <country>] [--search-num <1-100>] [--search-time <hour|day|week|month|year>] [--timeout <seconds>]
  firecrawl web        --query <keywords> [--country <country>] [--search-num <1-100>] [--search-time <hour|day|week|month|year>] [--timeout <seconds>]
  firecrawl news       --query <keywords> [--country <country>] [--search-num <1-100>] [--search-time <hour|day|week|month|year>] [--timeout <seconds>]
  firecrawl image      --query <keywords> [--country <country>] [--search-num <1-100>] [--search-time <hour|day|week|month|year>] [--timeout <seconds>]
  firecrawl scrape     --output <name> [--path <dir>] --url <url> [--include-tags <selectors>] [--exclude-tags <selectors>] [--empty-tags] [--skip-tls] [--headers <json-object>] [--timeout <seconds>]
  firecrawl credit-usage [--json] [--pretty]

The API key is read from FIRECRAWL_KEY.
The optional API base URL is read from FIRECRAWL_BASE_URL and defaults to https://api.firecrawl.dev/v2.

`)
}

func printSearchUsage(w io.Writer, name string) {
	fmt.Fprintf(w, `Usage:
  firecrawl %s --query <keywords> [--country <country>] [--search-num <1-100>] [--search-time <hour|day|week|month|year>] [--timeout <seconds>]

Parameters:
  --query        Search keywords. Required.
  --country      Country or region for search results. Optional. Supports names and ISO codes. Default is US.
  --search-num   Number of results to return. Optional. Legal range: 1-100. Default is 20.
  --search-time  Time filter. Optional. One of: "hour", "day", "week", "month", "year".
  --timeout      Request timeout in seconds. Optional. Must be > 0. Default is 120.

Output:
  Compact single-line JSON with success, data.web, data.news, data.images, and creditsUsed.

`, name)
}

func printScrapeUsage(w io.Writer) {
	fmt.Fprint(w, `Usage:
  firecrawl scrape --output <name> [--path <dir>] --url <url> [--include-tags <selectors>] [--exclude-tags <selectors>] [--empty-tags] [--skip-tls] [--headers <json-object>] [--timeout <seconds>]

Parameters:
  --output          Export name. Required. The result is saved as <output>.md.
  --path            Directory where the markdown export is saved. Optional. Supports absolute and relative paths. Default is the current directory.
  --url             Target webpage URL. Required.
  --include-tags    CSS selectors to include. Optional. Single selector, comma-separated string, or JSON string array.
  --exclude-tags    Additional CSS selectors to exclude. Optional. Single selector, comma-separated string, or JSON string array.
  --empty-tags      Clear the built-in exclude selector list while keeping user-provided --exclude-tags.
  --skip-tls        Skip TLS certificate verification for the upstream scrape target. Optional. Default is false.
  --headers         Root-level request headers as a JSON object, for example {"Authorization":"Bearer token","X-Trace-Id":"abc123"}.
  --timeout         Request timeout in seconds. Optional. Must be > 0. Default is 120.

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
  firecrawl credit-usage [--json] [--pretty]

Parameters:
  --json    Output JSON. Optional. JSON is the default output format.
  --pretty  Pretty-print JSON output. Optional.

Output:
  JSON response from /v2/team/credit-usage.

`)
}
