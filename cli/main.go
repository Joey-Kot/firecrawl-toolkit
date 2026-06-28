package main

import (
	"errors"
	"fmt"
	"io"
	"os"
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
