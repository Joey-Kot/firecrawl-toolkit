package main

import (
	"embed"
	"net/http"
	"time"
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
