package main

import (
	"net/url"
	"strconv"
	"strings"
)

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
