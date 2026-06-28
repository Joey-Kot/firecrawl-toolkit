package main

import (
	"flag"
	"fmt"
	"io"
	"strings"
)

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
