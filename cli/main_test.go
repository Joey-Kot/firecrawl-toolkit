package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

func TestSearchCommandOutputsCompactMappedJSON(t *testing.T) {
	t.Setenv(apiKeyEnv, "test-key")
	setMockHTTPClient(t, func(r *http.Request) (*http.Response, error) {
		assertRequestTimeout(t, r, defaultTimeoutSecs)
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("Authorization header = %q", got)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["country"] != "GB" {
			t.Fatalf("country = %v", payload["country"])
		}
		if payload["tbs"] != "qdr:m" {
			t.Fatalf("tbs = %v", payload["tbs"])
		}
		if payload["timeout"] != float64(120000) {
			t.Fatalf("timeout = %#v", payload["timeout"])
		}
		scrapeOptions := payload["scrapeOptions"].(map[string]any)
		if scrapeOptions["timeout"] != float64(120000) {
			t.Fatalf("scrapeOptions.timeout = %#v", scrapeOptions["timeout"])
		}
		return jsonResponse(200, `{"success":true,"data":{"web":[{"title":"w","description":"d","url":"u","extra":1}],"news":[{"title":"n","url":"nu","date":"today"}],"images":[{"title":"i","imageUrl":"iu","url":"ru"}]},"creditsUsed":3}`), nil
	})

	old := endpoints["search"]
	endpoints["search"] = "https://example.test/search"
	t.Cleanup(func() { endpoints["search"] = old })

	var stdout, stderr bytes.Buffer
	err := run([]string{"web", "--query", "ai", "--country", "United Kingdom", "--search-num", "5", "--search-time", "month"}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run returned error: %v; stderr=%s", err, stderr.String())
	}
	out := strings.TrimSpace(stdout.String())
	if strings.Contains(out, "\n") || strings.Contains(out, " ") {
		t.Fatalf("expected compact single-line JSON, got %q", out)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatal(err)
	}
	data := parsed["data"].(map[string]any)
	web := data["web"].([]any)[0].(map[string]any)
	if _, ok := web["extra"]; ok {
		t.Fatalf("unexpected extra field in mapped web result: %#v", web)
	}
	news := data["news"].([]any)[0].(map[string]any)
	if news["snippet"] != nil {
		t.Fatalf("missing snippet should map to nil, got %#v", news["snippet"])
	}
}

func TestSearchCommandUsesTimeoutFlagForRequestAndPayload(t *testing.T) {
	t.Setenv(apiKeyEnv, "test-key")
	setMockHTTPClient(t, func(r *http.Request) (*http.Response, error) {
		assertRequestTimeout(t, r, 7)
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["timeout"] != float64(7000) {
			t.Fatalf("timeout = %#v", payload["timeout"])
		}
		scrapeOptions := payload["scrapeOptions"].(map[string]any)
		if scrapeOptions["timeout"] != float64(7000) {
			t.Fatalf("scrapeOptions.timeout = %#v", scrapeOptions["timeout"])
		}
		return jsonResponse(200, `{"success":true,"data":{"web":[],"news":[],"images":[]}}`), nil
	})

	old := endpoints["search"]
	endpoints["search"] = "https://example.test/search"
	t.Cleanup(func() { endpoints["search"] = old })

	var stdout, stderr bytes.Buffer
	err := run([]string{"web", "--query", "ai", "--timeout", "7"}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run returned error: %v; stderr=%s", err, stderr.String())
	}
}

func TestSearchCommandRetriesRetryableFailures(t *testing.T) {
	t.Setenv(apiKeyEnv, "test-key")
	t.Setenv("FIRECRAWL_RETRY_BASE_DELAY", "0")
	calls := 0
	setMockHTTPClient(t, func(r *http.Request) (*http.Response, error) {
		calls++
		switch calls {
		case 1:
			return jsonResponse(502, `<html>temporary</html>`), nil
		case 2:
			return nil, errors.New("connection reset")
		default:
			return jsonResponse(200, `{"success":true,"data":{"web":[],"news":[],"images":[]}}`), nil
		}
	})

	old := endpoints["search"]
	endpoints["search"] = "https://example.test/search"
	t.Cleanup(func() { endpoints["search"] = old })

	var stdout, stderr bytes.Buffer
	err := run([]string{"web", "--query", "ai"}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run returned error: %v; stderr=%s", err, stderr.String())
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
}

func TestSearchCommandDoesNotRetryClientErrors(t *testing.T) {
	t.Setenv(apiKeyEnv, "test-key")
	t.Setenv("FIRECRAWL_RETRY_BASE_DELAY", "0")
	calls := 0
	setMockHTTPClient(t, func(r *http.Request) (*http.Response, error) {
		calls++
		return jsonResponse(400, `{"message":"bad request"}`), nil
	})

	old := endpoints["search"]
	endpoints["search"] = "https://example.test/search"
	t.Cleanup(func() { endpoints["search"] = old })

	var stdout, stderr bytes.Buffer
	err := run([]string{"web", "--query", "ai"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected search failure")
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestCreditUsageCommandOutputsJSON(t *testing.T) {
	t.Setenv(apiKeyEnv, "test-key")
	setMockHTTPClient(t, func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("Authorization header = %q", got)
		}
		return jsonResponse(200, `{"success":true,"data":{"remainingCredits":1000,"planCredits":500000,"billingPeriodStart":"2025-01-01T00:00:00Z","billingPeriodEnd":"2025-01-31T23:59:59Z"}}`), nil
	})

	old := endpoints["credit-usage"]
	endpoints["credit-usage"] = "https://example.test/team/credit-usage"
	t.Cleanup(func() { endpoints["credit-usage"] = old })

	var stdout, stderr bytes.Buffer
	err := run([]string{"credit-usage"}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run returned error: %v; stderr=%s", err, stderr.String())
	}
	out := strings.TrimSpace(stdout.String())
	if strings.Contains(out, "\n") || strings.Contains(out, "  ") {
		t.Fatalf("expected compact JSON, got %q", out)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatal(err)
	}
	data := parsed["data"].(map[string]any)
	if data["remainingCredits"] != float64(1000) {
		t.Fatalf("remainingCredits = %#v", data["remainingCredits"])
	}

	stdout.Reset()
	stderr.Reset()
	err = run([]string{"credit-usage", "--json", "--pretty"}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run returned error: %v; stderr=%s", err, stderr.String())
	}
	pretty := stdout.String()
	if !strings.Contains(pretty, "\n  \"data\": {") || !strings.Contains(pretty, "\n    \"remainingCredits\": 1000") {
		t.Fatalf("expected pretty JSON, got %q", pretty)
	}
}

func TestCreditUsageCommandRetriesServerErrors(t *testing.T) {
	t.Setenv(apiKeyEnv, "test-key")
	t.Setenv("FIRECRAWL_RETRY_BASE_DELAY", "0")
	calls := 0
	setMockHTTPClient(t, func(r *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return jsonResponse(503, `{"message":"temporary"}`), nil
		}
		return jsonResponse(200, `{"success":true,"data":{"remainingCredits":10}}`), nil
	})

	old := endpoints["credit-usage"]
	endpoints["credit-usage"] = "https://example.test/team/credit-usage"
	t.Cleanup(func() { endpoints["credit-usage"] = old })

	var stdout, stderr bytes.Buffer
	err := run([]string{"credit-usage"}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run returned error: %v; stderr=%s", err, stderr.String())
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
}

func TestAPIKeyParsingSplitsCommasAndIgnoresEmptyEntries(t *testing.T) {
	keys := parseAPIKeys(" key-1, key-2 ,,key-3 ")
	if strings.Join(keys, ",") != "key-1,key-2,key-3" {
		t.Fatalf("parseAPIKeys returned %#v", keys)
	}
	if keys := parseAPIKeys(" , "); len(keys) != 0 {
		t.Fatalf("expected empty key list, got %#v", keys)
	}
}

func TestRequestsUseAKeyFromCommaSeparatedPool(t *testing.T) {
	t.Setenv(apiKeyEnv, "key-1, key-2")
	setMockHTTPClient(t, func(r *http.Request) (*http.Response, error) {
		got := r.Header.Get("Authorization")
		if got != "Bearer key-1" && got != "Bearer key-2" {
			t.Fatalf("Authorization header = %q", got)
		}
		return jsonResponse(200, `{"success":true,"data":{"web":[],"news":[],"images":[]}}`), nil
	})

	old := endpoints["search"]
	endpoints["search"] = "https://example.test/search"
	t.Cleanup(func() { endpoints["search"] = old })

	var stdout, stderr bytes.Buffer
	err := run([]string{"web", "--query", "ai"}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run returned error: %v; stderr=%s", err, stderr.String())
	}
}

func TestEndpointURLUsesConfiguredBaseURL(t *testing.T) {
	t.Setenv(baseURLEnv, "https://self-hosted.example/api/v2/")

	cases := map[string]string{
		"search":       "https://self-hosted.example/api/v2/search",
		"scrape":       "https://self-hosted.example/api/v2/scrape",
		"credit-usage": "https://self-hosted.example/api/v2/team/credit-usage",
	}
	for endpointName, want := range cases {
		if got := endpointURL(endpointName); got != want {
			t.Fatalf("endpointURL(%q) = %q, want %q", endpointName, got, want)
		}
	}
}

func TestScrapeCommandWritesMarkdownFileOnSuccess(t *testing.T) {
	t.Setenv(apiKeyEnv, "test-key")
	setMockHTTPClient(t, func(r *http.Request) (*http.Response, error) {
		assertRequestTimeout(t, r, 7)
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["timeout"] != float64(7000) {
			t.Fatalf("timeout = %#v", payload["timeout"])
		}
		if _, ok := payload["startIndex"]; ok {
			t.Fatal("startIndex must not be forwarded upstream")
		}
		if _, ok := payload["maxCharacters"]; ok {
			t.Fatal("maxCharacters must not be forwarded upstream")
		}
		includeTags := payload["includeTags"].([]any)
		if len(includeTags) != 2 || includeTags[0] != "article" || includeTags[1] != ".content" {
			t.Fatalf("includeTags = %#v", includeTags)
		}
		excludeTags := payload["excludeTags"].([]any)
		if len(excludeTags) != 1 || excludeTags[0] != ".nav" {
			t.Fatalf("excludeTags = %#v", excludeTags)
		}
		headers := payload["headers"].(map[string]any)
		if headers["X-Trace-Id"] != "abc123" {
			t.Fatalf("headers = %#v", headers)
		}
		return jsonResponse(200, `{"success":true,"data":{"markdown":"hello%20world%21\nnext\\nline","metadata":{"title":"T","description":"D","url":"https://canonical.example/page","language":"en","creditsUsed":2}}}`), nil
	})

	old := endpoints["scrape"]
	endpoints["scrape"] = "https://example.test/scrape"
	t.Cleanup(func() { endpoints["scrape"] = old })

	dir := t.TempDir()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	var stdout, stderr bytes.Buffer
	err = run([]string{
		"scrape",
		"--output", "page",
		"--url", "https://example.com",
		"--include-tags", `["article",".content"]`,
		"--exclude-tags", ".nav",
		"--empty-tags",
		"--headers", `{"X-Trace-Id":"abc123"}`,
		"--timeout", "7",
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run returned error: %v; stderr=%s", err, stderr.String())
	}
	if strings.TrimSpace(stdout.String()) != "true" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	content, err := os.ReadFile(filepath.Join(dir, "page.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	for _, want := range []string{
		"## title: T",
		"## description: D",
		"## url: https://canonical.example/page",
		"## language: en",
		"## creditsUsed: 2",
		"hello world!\nnext\nline",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("export content missing %q:\n%s", want, text)
		}
	}
}

func TestScrapeCommandWritesMarkdownFileToPath(t *testing.T) {
	t.Setenv(apiKeyEnv, "test-key")
	setMockHTTPClient(t, func(r *http.Request) (*http.Response, error) {
		return jsonResponse(200, `{"success":true,"data":{"markdown":"hello","metadata":{"title":"T","url":"https://example.com","creditsUsed":1}}}`), nil
	})

	old := endpoints["scrape"]
	endpoints["scrape"] = "https://example.test/scrape"
	t.Cleanup(func() { endpoints["scrape"] = old })

	dir := t.TempDir()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	var stdout, stderr bytes.Buffer
	err = run([]string{
		"scrape",
		"--output", "page",
		"--path", filepath.Join("exports", "pages"),
		"--url", "https://example.com",
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run returned error: %v; stderr=%s", err, stderr.String())
	}
	if strings.TrimSpace(stdout.String()) != "true" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "exports", "pages")); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(dir, "exports", "pages", "page.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "hello") {
		t.Fatalf("export content = %q", string(content))
	}
}

func TestScrapeCommandRetriesWithoutSelectorsWhenMarkdownIsEmpty(t *testing.T) {
	t.Setenv(apiKeyEnv, "test-key")
	calls := 0
	setMockHTTPClient(t, func(r *http.Request) (*http.Response, error) {
		calls++
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		switch calls {
		case 1:
			if _, ok := payload["includeTags"]; !ok {
				t.Fatal("first request should include includeTags")
			}
			if _, ok := payload["excludeTags"]; !ok {
				t.Fatal("first request should include excludeTags")
			}
			return jsonResponse(200, `{"success":true,"data":{"markdown":"","metadata":{"title":"empty"}}}`), nil
		case 2:
			if _, ok := payload["includeTags"]; ok {
				t.Fatalf("fallback request should omit includeTags: %#v", payload)
			}
			if _, ok := payload["excludeTags"]; ok {
				t.Fatalf("fallback request should omit excludeTags: %#v", payload)
			}
			return jsonResponse(200, `{"success":true,"data":{"markdown":"fallback","metadata":{"title":"T"}}}`), nil
		default:
			t.Fatalf("unexpected request %d", calls)
			return nil, nil
		}
	})

	old := endpoints["scrape"]
	endpoints["scrape"] = "https://example.test/scrape"
	t.Cleanup(func() { endpoints["scrape"] = old })

	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	err := run([]string{
		"scrape",
		"--output", "page",
		"--path", dir,
		"--url", "https://example.com",
		"--include-tags", "article",
		"--exclude-tags", ".nav",
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run returned error: %v; stderr=%s", err, stderr.String())
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
	content, err := os.ReadFile(filepath.Join(dir, "page.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "fallback") {
		t.Fatalf("export content = %q", string(content))
	}
}

func TestScrapeCommandCreatesPathBeforeRequest(t *testing.T) {
	t.Setenv(apiKeyEnv, "test-key")
	called := false
	setMockHTTPClient(t, func(r *http.Request) (*http.Response, error) {
		called = true
		return jsonResponse(200, `{"success":true,"data":{"markdown":"hello","metadata":{}}}`), nil
	})

	old := endpoints["scrape"]
	endpoints["scrape"] = "https://example.test/scrape"
	t.Cleanup(func() { endpoints["scrape"] = old })

	dir := t.TempDir()
	blockedPath := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(blockedPath, []byte("file"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	err := run([]string{
		"scrape",
		"--output", "page",
		"--path", blockedPath,
		"--url", "https://example.com",
	}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected path creation failure")
	}
	if called {
		t.Fatal("scrape request was called before output path was created")
	}
	if !strings.Contains(err.Error(), "failed to create output path") {
		t.Fatalf("error = %v", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestBuildScrapePayloadEmptyTags(t *testing.T) {
	payload := buildScrapePayload("https://example.com", nil, []string{".nav", "script", ".nav"}, true, nil, defaultTimeoutSecs)
	excludeTags := payload["excludeTags"].([]string)
	if strings.Join(excludeTags, ",") != ".nav,script" {
		t.Fatalf("excludeTags = %#v", excludeTags)
	}
	if payload["timeout"] != int64(120000) {
		t.Fatalf("timeout = %#v", payload["timeout"])
	}

	payload = buildScrapePayload("https://example.com", nil, nil, true, nil, defaultTimeoutSecs)
	excludeTags = payload["excludeTags"].([]string)
	if len(excludeTags) != 0 {
		t.Fatalf("excludeTags = %#v", excludeTags)
	}

	payload = buildScrapePayload("https://example.com", nil, []string{".nav"}, false, nil, defaultTimeoutSecs)
	excludeTags = payload["excludeTags"].([]string)
	if !containsString(excludeTags, "script") || !containsString(excludeTags, ".nav") {
		t.Fatalf("excludeTags should include built-in and user selectors, got %#v", excludeTags)
	}
}

func TestScrapeFailureDoesNotOverwriteExistingFile(t *testing.T) {
	t.Setenv(apiKeyEnv, "test-key")
	setMockHTTPClient(t, func(r *http.Request) (*http.Response, error) {
		return jsonResponse(200, `{"success":false,"message":"upstream failed"}`), nil
	})

	old := endpoints["scrape"]
	endpoints["scrape"] = "https://example.test/scrape"
	t.Cleanup(func() { endpoints["scrape"] = old })

	dir := t.TempDir()
	path := filepath.Join(dir, "page.md")
	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	var stdout, stderr bytes.Buffer
	err = run([]string{"scrape", "--output", "page", "--url", "https://example.com"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected scrape failure")
	}
	if !strings.Contains(stdout.String(), "false\nupstream failed") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "original" {
		t.Fatalf("file was overwritten: %q", string(content))
	}
}

func TestValidation(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run([]string{"aggregated", "--query", "ai", "--search-num", "101"}, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "--search-num") {
		t.Fatalf("expected search-num validation error, got %v", err)
	}

	stdout.Reset()
	stderr.Reset()
	err = run([]string{"scrape", "--output", "x", "--url", "https://example.com", "--headers", `[]`}, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "--headers") {
		t.Fatalf("expected headers validation error, got %v", err)
	}

	stdout.Reset()
	stderr.Reset()
	run([]string{"scrape", "--help"}, &stdout, &stderr)
	if strings.Contains(stderr.String(), "start-index") || strings.Contains(stderr.String(), "max-characters") {
		t.Fatalf("scrape usage should not mention removed truncation flags:\n%s", stderr.String())
	}

	for _, removedFlag := range []string{"--start-index", "--max-characters"} {
		stdout.Reset()
		stderr.Reset()
		err = run([]string{"scrape", "--output", "x", "--url", "https://example.com", removedFlag, "1"}, &stdout, &stderr)
		if err == nil || !strings.Contains(stderr.String(), "flag provided but not defined") {
			t.Fatalf("expected %s to be rejected as an unknown flag, err=%v stderr=%s", removedFlag, err, stderr.String())
		}
	}

	stdout.Reset()
	stderr.Reset()
	err = run([]string{"web", "--query", "ai", "--timeout", "0"}, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "--timeout") {
		t.Fatalf("expected timeout validation error, got %v", err)
	}
}

func TestCountryAliases(t *testing.T) {
	cases := map[string]string{
		"U.S.":            "US",
		"United Kingdom":  "GB",
		"P.R.C.":          "CN",
		"Viet Nam":        "VN",
		"Congo-Kinshasa":  "CD",
		"Aland Islands":   "AX",
		"Reunion":         "RE",
		"Cote d'Ivoire":   "CI",
		"unknown-country": "US",
	}
	for input, want := range cases {
		if got := getCountryCodeAlpha2(input); got != want {
			t.Fatalf("getCountryCodeAlpha2(%q) = %q, want %q", input, got, want)
		}
	}
	if len(countryAliases) == 0 {
		t.Fatal("expected embedded country aliases to load")
	}
}

func TestCountryAliasDataMatchesPythonPackage(t *testing.T) {
	cliData, err := os.ReadFile(filepath.Join("data", "country_aliases.json"))
	if err != nil {
		t.Fatal(err)
	}
	pythonData, err := os.ReadFile(filepath.Join("..", "firecrawl_toolkit", "data", "country_aliases.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(cliData, pythonData) {
		t.Fatal("cli/data/country_aliases.json differs from firecrawl_toolkit/data/country_aliases.json")
	}
}

func TestEveryAliasAndFoldedAliasResolves(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("data", "country_aliases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string][]string
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	for code, aliases := range raw {
		want := strings.ToUpper(code)
		for _, alias := range aliases {
			if got := getCountryCodeAlpha2(alias); got != want {
				t.Fatalf("getCountryCodeAlpha2(%q) = %q, want %q", alias, got, want)
			}
			folded := foldDiacritics(alias)
			if folded != alias {
				if got := getCountryCodeAlpha2(folded); got != want {
					t.Fatalf("getCountryCodeAlpha2(%q folded from %q) = %q, want %q", folded, alias, got, want)
				}
			}
		}
	}
}

func foldDiacritics(value string) string {
	value = norm.NFKD.String(value)
	var b strings.Builder
	for _, r := range value {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return fn(r)
}

func setMockHTTPClient(t *testing.T, fn roundTripFunc) {
	t.Helper()
	old := httpClient
	httpClient = &http.Client{Transport: fn}
	t.Cleanup(func() { httpClient = old })
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func assertRequestTimeout(t *testing.T, r *http.Request, wantSeconds int) {
	t.Helper()
	deadline, ok := r.Context().Deadline()
	if !ok {
		t.Fatal("request context has no deadline")
	}
	remaining := time.Until(deadline)
	if remaining <= 0 || remaining > time.Duration(wantSeconds)*time.Second {
		t.Fatalf("request timeout = %s, want <= %ds and > 0", remaining, wantSeconds)
	}
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
