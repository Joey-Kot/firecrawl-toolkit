package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

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
