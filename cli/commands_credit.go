package main

import (
	"flag"
	"fmt"
	"io"
	"strings"
)

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
