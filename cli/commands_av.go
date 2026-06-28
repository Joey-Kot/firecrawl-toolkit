package main

import (
	"flag"
	"fmt"
	"io"
	"strings"
)

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
