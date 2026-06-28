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
	if strings.Contains(out, "\n") || strings.Contains(out, ": ") {
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

func TestFormatJSONDoesNotEscapeHTMLCharacters(t *testing.T) {
	payload := map[string]any{"url": "https://storage.example/file.mp3?a=1&b=<tag>"}

	compact := compactJSON(payload)
	if strings.Contains(compact, `\u0026`) || strings.Contains(compact, `\u003c`) || strings.Contains(compact, `\u003e`) {
		t.Fatalf("compact JSON should not HTML-escape characters: %s", compact)
	}
	if !strings.Contains(compact, "a=1&b=<tag>") {
		t.Fatalf("compact JSON missing raw URL characters: %s", compact)
	}

	pretty := formatJSON(payload, true)
	if strings.Contains(pretty, `\u0026`) || strings.Contains(pretty, `\u003c`) || strings.Contains(pretty, `\u003e`) {
		t.Fatalf("pretty JSON should not HTML-escape characters: %s", pretty)
	}
	if !strings.Contains(pretty, "a=1&b=<tag>") {
		t.Fatalf("pretty JSON missing raw URL characters: %s", pretty)
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

func TestScholarCommandOutputsCompactMappedJSON(t *testing.T) {
	t.Setenv(apiKeyEnv, "test-key")
	setMockHTTPClient(t, func(r *http.Request) (*http.Response, error) {
		assertRequestTimeout(t, r, 8)
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s", r.Method)
		}
		if got := r.URL.Path; got != "/search/research/papers" {
			t.Fatalf("path = %s", got)
		}
		values := r.URL.Query()
		if values.Get("query") != "AI" {
			t.Fatalf("query = %q", values.Get("query"))
		}
		if values.Get("k") != "3" {
			t.Fatalf("k = %q", values.Get("k"))
		}
		if values.Get("categories") != "cs.CY,cs.AI" {
			t.Fatalf("categories = %q", values.Get("categories"))
		}
		if values.Get("from") != "2000-05-28" {
			t.Fatalf("from = %q", values.Get("from"))
		}
		if values.Get("to") != "2026-06-28" {
			t.Fatalf("to = %q", values.Get("to"))
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("Authorization header = %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("Content-Type = %q", got)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["timeout"] != float64(8000) {
			t.Fatalf("timeout = %#v", payload["timeout"])
		}
		return jsonResponse(200, `{"success":true,"results":[{"paperId":"2581735124241874","primaryId":"arxiv:2307.10057","ids":{"arxiv":["2307.10057"]},"title":"Ethics in the Age of AI","abstract":"Ethics in AI has become a debated topic.","score":0.956892745058914,"extra":"ignored"}]}`), nil
	})

	old := endpoints["scholar"]
	endpoints["scholar"] = "https://example.test/search/research/papers"
	t.Cleanup(func() { endpoints["scholar"] = old })

	var stdout, stderr bytes.Buffer
	err := run([]string{
		"scholar",
		"--query", "AI",
		"--search-num", "3",
		"--categories", "cs.CY,cs.AI",
		"--time-from", "2000-05-28",
		"--time-to", "2026-06-28",
		"--timeout", "8",
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run returned error: %v; stderr=%s", err, stderr.String())
	}
	out := strings.TrimSpace(stdout.String())
	if strings.Contains(out, "\n") || strings.Contains(out, ": ") {
		t.Fatalf("expected compact single-line JSON, got %q", out)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed["success"] != true {
		t.Fatalf("success = %#v", parsed["success"])
	}
	data := parsed["data"].(map[string]any)
	scholar := data["scholar"].([]any)
	if len(scholar) != 1 {
		t.Fatalf("scholar = %#v", scholar)
	}
	paper := scholar[0].(map[string]any)
	for _, field := range []string{"title", "abstract", "paperId", "primaryId", "score"} {
		if _, ok := paper[field]; !ok {
			t.Fatalf("paper missing %s: %#v", field, paper)
		}
	}
	if _, ok := paper["ids"]; ok {
		t.Fatalf("unexpected ids field in mapped paper: %#v", paper)
	}
	if _, ok := paper["extra"]; ok {
		t.Fatalf("unexpected extra field in mapped paper: %#v", paper)
	}
}

func TestScholarCommandRequiresQuery(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run([]string{"scholar", "--search-num", "3"}, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "--query is required") {
		t.Fatalf("expected --query validation error, got err=%v stdout=%s stderr=%s", err, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "firecrawl scholar --query <keywords> [--search-num <1-500>]") {
		t.Fatalf("stderr missing usage: %s", stderr.String())
	}
}

func TestScholarCommandDefaultsSearchNumToFive(t *testing.T) {
	t.Setenv(apiKeyEnv, "test-key")
	setMockHTTPClient(t, func(r *http.Request) (*http.Response, error) {
		if got := r.URL.Query().Get("k"); got != "5" {
			t.Fatalf("k = %q", got)
		}
		return jsonResponse(200, `{"success":true,"results":[]}`), nil
	})

	old := endpoints["scholar"]
	endpoints["scholar"] = "https://example.test/search/research/papers"
	t.Cleanup(func() { endpoints["scholar"] = old })

	var stdout, stderr bytes.Buffer
	err := run([]string{"scholar", "--query", "AI"}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run returned error: %v; stderr=%s", err, stderr.String())
	}
}

func TestScholarCommandValidatesSearchNumRange(t *testing.T) {
	for _, value := range []string{"0", "501"} {
		var stdout, stderr bytes.Buffer
		err := run([]string{"scholar", "--query", "AI", "--search-num", value}, &stdout, &stderr)
		if err == nil || !strings.Contains(err.Error(), "--search-num must be an integer from 1 to 500") {
			t.Fatalf("expected search-num range error, got err=%v stdout=%s stderr=%s", err, stdout.String(), stderr.String())
		}
	}
}

func TestScholarUsageShowsDateFormatExamples(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run([]string{"scholar", "-h"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected flag package help to return an error")
	}
	usage := stderr.String()
	for _, want := range []string{
		"Format: yyyy-MM-dd, for example 2000-05-28.",
		"Format: yyyy-MM-dd, for example 2026-06-28.",
	} {
		if !strings.Contains(usage, want) {
			t.Fatalf("scholar usage missing %q:\n%s", want, usage)
		}
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
		"scholar":      "https://self-hosted.example/api/v2/search/research/papers",
		"scrape":       "https://self-hosted.example/api/v2/scrape",
		"parse":        "https://self-hosted.example/api/v2/parse",
		"credit-usage": "https://self-hosted.example/api/v2/team/credit-usage",
	}
	for endpointName, want := range cases {
		if got := endpointURL(endpointName); got != want {
			t.Fatalf("endpointURL(%q) = %q, want %q", endpointName, got, want)
		}
	}
}

func TestRootUsageIncludesAVScrapeTimeout(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run([]string{"--help"}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run returned error: %v; stderr=%s", err, stderr.String())
	}
	usage := stdout.String()
	for _, want := range []string{
		"firecrawl scholar    --query <keywords> [--search-num <1-500>] [--categories <categories>] [--time-from <date>] [--time-to <date>] [--timeout <seconds>]",
		"firecrawl scrape     --output <name> [--path <dir>] --url <url> [--include-tags <selectors>] [--exclude-tags <selectors>] [--empty-tags] [--scroll] [--skip-tls] [--headers <json-object>] [--headers-file <file>] [--timeout <seconds>]",
		"firecrawl audio-scrape --url <url> [--timeout <seconds>]",
		"firecrawl video-scrape --url <url> [--timeout <seconds>]",
	} {
		if !strings.Contains(usage, want) {
			t.Fatalf("root usage missing %q:\n%s", want, usage)
		}
	}
}

func TestAudioScrapeCommandOutputsCompactMappedJSON(t *testing.T) {
	t.Setenv(apiKeyEnv, "test-key")
	setMockHTTPClient(t, func(r *http.Request) (*http.Response, error) {
		assertRequestTimeout(t, r, defaultTimeoutSecs)
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s", r.Method)
		}
		if got := r.URL.String(); got != "https://example.test/scrape" {
			t.Fatalf("url = %s", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("Authorization header = %q", got)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["url"] != "https://www.youtube.com/watch?v=dQw4w9WgXcQ" {
			t.Fatalf("url payload = %#v", payload["url"])
		}
		formats := payload["formats"].([]any)
		if len(formats) != 1 || formats[0] != "audio" {
			t.Fatalf("formats = %#v", formats)
		}
		if payload["timeout"] != float64(120000) {
			t.Fatalf("timeout = %#v", payload["timeout"])
		}
		if len(payload) != 3 {
			t.Fatalf("unexpected audio-scrape payload fields: %#v", payload)
		}
		return jsonResponse(200, `{"success":true,"data":{"metadata":{"title":"Video title","description":"Video description","creditsUsed":5},"audio":"https://storage.example/audio.mp3"}}`), nil
	})

	old := endpoints["scrape"]
	endpoints["scrape"] = "https://example.test/scrape"
	t.Cleanup(func() { endpoints["scrape"] = old })

	var stdout, stderr bytes.Buffer
	err := run([]string{"audio-scrape", "--url", "https://www.youtube.com/watch?v=dQw4w9WgXcQ"}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run returned error: %v; stderr=%s", err, stderr.String())
	}
	out := strings.TrimSpace(stdout.String())
	if strings.Contains(out, "\n") || strings.Contains(out, ": ") {
		t.Fatalf("expected compact single-line JSON, got %q", out)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed["creditsUsed"] != float64(5) {
		t.Fatalf("creditsUsed = %#v", parsed["creditsUsed"])
	}
	if parsed["title"] != "Video title" {
		t.Fatalf("title = %#v", parsed["title"])
	}
	if parsed["description"] != "Video description" {
		t.Fatalf("description = %#v", parsed["description"])
	}
	if parsed["audio"] != "https://storage.example/audio.mp3" {
		t.Fatalf("audio = %#v", parsed["audio"])
	}
	if parsed["success"] != true {
		t.Fatalf("success = %#v", parsed["success"])
	}
}

func TestVideoScrapeCommandOutputsCompactMappedJSON(t *testing.T) {
	t.Setenv(apiKeyEnv, "test-key")
	setMockHTTPClient(t, func(r *http.Request) (*http.Response, error) {
		assertRequestTimeout(t, r, defaultTimeoutSecs)
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s", r.Method)
		}
		if got := r.URL.String(); got != "https://example.test/scrape" {
			t.Fatalf("url = %s", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("Authorization header = %q", got)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["url"] != "https://www.youtube.com/watch?v=dQw4w9WgXcQ" {
			t.Fatalf("url payload = %#v", payload["url"])
		}
		formats := payload["formats"].([]any)
		if len(formats) != 1 || formats[0] != "video" {
			t.Fatalf("formats = %#v", formats)
		}
		if payload["timeout"] != float64(120000) {
			t.Fatalf("timeout = %#v", payload["timeout"])
		}
		if len(payload) != 3 {
			t.Fatalf("unexpected video-scrape payload fields: %#v", payload)
		}
		return jsonResponse(200, `{"success":true,"data":{"metadata":{"title":"Video title","description":"Video description","creditsUsed":5},"video":"https://storage.example/video.mp4"}}`), nil
	})

	old := endpoints["scrape"]
	endpoints["scrape"] = "https://example.test/scrape"
	t.Cleanup(func() { endpoints["scrape"] = old })

	var stdout, stderr bytes.Buffer
	err := run([]string{"video-scrape", "--url", "https://www.youtube.com/watch?v=dQw4w9WgXcQ"}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run returned error: %v; stderr=%s", err, stderr.String())
	}
	out := strings.TrimSpace(stdout.String())
	if strings.Contains(out, "\n") || strings.Contains(out, ": ") {
		t.Fatalf("expected compact single-line JSON, got %q", out)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed["creditsUsed"] != float64(5) {
		t.Fatalf("creditsUsed = %#v", parsed["creditsUsed"])
	}
	if parsed["title"] != "Video title" {
		t.Fatalf("title = %#v", parsed["title"])
	}
	if parsed["description"] != "Video description" {
		t.Fatalf("description = %#v", parsed["description"])
	}
	if parsed["video"] != "https://storage.example/video.mp4" {
		t.Fatalf("video = %#v", parsed["video"])
	}
	if parsed["success"] != true {
		t.Fatalf("success = %#v", parsed["success"])
	}
}

func TestAVScrapeCommandUsesTimeoutFlagForRequestAndPayload(t *testing.T) {
	t.Setenv(apiKeyEnv, "test-key")
	setMockHTTPClient(t, func(r *http.Request) (*http.Response, error) {
		assertRequestTimeout(t, r, 6)
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["timeout"] != float64(6000) {
			t.Fatalf("timeout = %#v", payload["timeout"])
		}
		return jsonResponse(200, `{"success":true,"data":{"metadata":{"title":"Video title","creditsUsed":5},"video":"https://storage.example/video.mp4"}}`), nil
	})

	old := endpoints["scrape"]
	endpoints["scrape"] = "https://example.test/scrape"
	t.Cleanup(func() { endpoints["scrape"] = old })

	var stdout, stderr bytes.Buffer
	err := run([]string{"video-scrape", "--url", "https://www.youtube.com/watch?v=dQw4w9WgXcQ", "--timeout", "6"}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run returned error: %v; stderr=%s", err, stderr.String())
	}
}

func TestAVScrapeCommandsRequireURL(t *testing.T) {
	for _, commandName := range []string{"audio-scrape", "video-scrape"} {
		t.Run(commandName, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := run([]string{commandName}, &stdout, &stderr)
			if err == nil || !strings.Contains(err.Error(), "--url is required") {
				t.Fatalf("expected --url validation error, got err=%v stdout=%s stderr=%s", err, stdout.String(), stderr.String())
			}
			if !strings.Contains(stderr.String(), "firecrawl "+commandName+" --url <url>") {
				t.Fatalf("stderr missing usage: %s", stderr.String())
			}
		})
	}
}

func TestParseURLCommandWritesMarkdownFileOnSuccess(t *testing.T) {
	t.Setenv(apiKeyEnv, "test-key")
	setMockHTTPClient(t, func(r *http.Request) (*http.Response, error) {
		assertRequestTimeout(t, r, 7)
		if r.URL.String() != "https://example.test/scrape" {
			t.Fatalf("url = %s", r.URL.String())
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("Content-Type = %q", got)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["url"] != "https://example.com/file.xlsx" {
			t.Fatalf("url payload = %#v", payload["url"])
		}
		formats := payload["formats"].([]any)
		if len(formats) != 1 || formats[0] != "markdown" {
			t.Fatalf("formats = %#v", formats)
		}
		parsers := payload["parsers"].([]any)
		if len(parsers) != 1 || parsers[0] != "pdf" {
			t.Fatalf("parsers = %#v", parsers)
		}
		if payload["removeBase64Images"] != false {
			t.Fatalf("removeBase64Images = %#v", payload["removeBase64Images"])
		}
		if payload["skipTlsVerification"] != true {
			t.Fatalf("skipTlsVerification = %#v", payload["skipTlsVerification"])
		}
		if payload["timeout"] != float64(7000) {
			t.Fatalf("timeout = %#v", payload["timeout"])
		}
		return jsonResponse(200, `{"success":true,"data":{"markdown":"hello%20world%21\\nnext","metadata":{"title":"Document","url":"https://example.com/file.xlsx","language":"en","creditsUsed":1}}}`), nil
	})

	old := endpoints["scrape"]
	endpoints["scrape"] = "https://example.test/scrape"
	t.Cleanup(func() { endpoints["scrape"] = old })

	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	err := run([]string{
		"parse",
		"--url", "https://example.com/file.xlsx",
		"--output", "parsed",
		"--path", dir,
		"--skip-tls",
		"--timeout", "7",
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run returned error: %v; stderr=%s", err, stderr.String())
	}
	if strings.TrimSpace(stdout.String()) != "true" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	content, err := os.ReadFile(filepath.Join(dir, "parsed.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	for _, want := range []string{
		"## title: Document",
		"## url: https://example.com/file.xlsx",
		"## language: en",
		"## creditsUsed: 1",
		"hello world!\nnext",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("export content missing %q:\n%s", want, text)
		}
	}
}

func TestParseFileCommandUploadsMultipartAndWritesMarkdownFile(t *testing.T) {
	t.Setenv(apiKeyEnv, "test-key")
	setMockHTTPClient(t, func(r *http.Request) (*http.Response, error) {
		assertRequestTimeout(t, r, 9)
		if r.URL.String() != "https://example.test/parse" {
			t.Fatalf("url = %s", r.URL.String())
		}
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data; boundary=") {
			t.Fatalf("Content-Type = %q", r.Header.Get("Content-Type"))
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		files := r.MultipartForm.File["file"]
		if len(files) != 1 || files[0].Filename != "input.xlsx" {
			t.Fatalf("uploaded files = %#v", files)
		}
		uploaded, err := files[0].Open()
		if err != nil {
			t.Fatal(err)
		}
		defer uploaded.Close()
		body, err := io.ReadAll(uploaded)
		if err != nil {
			t.Fatal(err)
		}
		if string(body) != "xlsx bytes" {
			t.Fatalf("uploaded body = %q", string(body))
		}
		var options map[string]any
		if err := json.Unmarshal([]byte(r.FormValue("options")), &options); err != nil {
			t.Fatal(err)
		}
		formats := options["formats"].([]any)
		if len(formats) != 1 || formats[0] != "markdown" {
			t.Fatalf("formats = %#v", formats)
		}
		parsers := options["parsers"].([]any)
		if len(parsers) != 1 || parsers[0] != "pdf" {
			t.Fatalf("parsers = %#v", parsers)
		}
		if options["removeBase64Images"] != false {
			t.Fatalf("removeBase64Images = %#v", options["removeBase64Images"])
		}
		if options["timeout"] != float64(9000) {
			t.Fatalf("timeout = %#v", options["timeout"])
		}
		if _, ok := options["skipTlsVerification"]; ok {
			t.Fatalf("skipTlsVerification should not be included for file mode: %#v", options)
		}
		return jsonResponse(200, `{"success":true,"data":{"markdown":"table\\nrow","metadata":{"title":"Document","url":"https://parse.example/uploads/input.xlsx","language":"en","creditsUsed":1}}}`), nil
	})

	old := endpoints["parse"]
	endpoints["parse"] = "https://example.test/parse"
	t.Cleanup(func() { endpoints["parse"] = old })

	dir := t.TempDir()
	inputPath := filepath.Join(dir, "input.xlsx")
	if err := os.WriteFile(inputPath, []byte("xlsx bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	err := run([]string{
		"parse",
		"--file", inputPath,
		"--output", "parsed-file",
		"--path", dir,
		"--skip-tls",
		"--timeout", "9",
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run returned error: %v; stderr=%s", err, stderr.String())
	}
	if strings.TrimSpace(stdout.String()) != "true" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	content, err := os.ReadFile(filepath.Join(dir, "parsed-file.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	if !strings.Contains(text, "## url: https://parse.example/uploads/input.xlsx") || !strings.Contains(text, "table\nrow") {
		t.Fatalf("export content = %q", text)
	}
}

func TestParseCommandRequiresExactlyOneInput(t *testing.T) {
	for _, args := range [][]string{
		{"parse", "--output", "x"},
		{"parse", "--output", "x", "--url", "https://example.com/file.pdf", "--file", "file.pdf"},
	} {
		var stdout, stderr bytes.Buffer
		err := run(args, &stdout, &stderr)
		if err == nil {
			t.Fatalf("expected validation error for args %#v", args)
		}
		if !strings.Contains(stderr.String(), "firecrawl parse (--url <url> | --file <file>)") {
			t.Fatalf("stderr missing usage: %s", stderr.String())
		}
	}
}

func TestParseFileCommandRejectsUnsupportedExtension(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "input.txt")
	if err := os.WriteFile(inputPath, []byte("text"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	err := run([]string{
		"parse",
		"--file", inputPath,
		"--output", "parsed",
	}, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "--file extension must be one of") {
		t.Fatalf("expected extension validation error, got err=%v stdout=%s stderr=%s", err, stdout.String(), stderr.String())
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
		if payload["skipTlsVerification"] != false {
			t.Fatalf("skipTlsVerification = %#v", payload["skipTlsVerification"])
		}
		if _, ok := payload["startIndex"]; ok {
			t.Fatal("startIndex must not be forwarded upstream")
		}
		if _, ok := payload["maxCharacters"]; ok {
			t.Fatal("maxCharacters must not be forwarded upstream")
		}
		if _, ok := payload["actions"]; ok {
			t.Fatalf("actions should be omitted by default: %#v", payload["actions"])
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

func TestScrapeCommandUsesHeadersFile(t *testing.T) {
	tests := []struct {
		name string
		body string
		want map[string]string
	}{
		{
			name: "json",
			body: `{"X-Trace-Id":"abc123","Authorization":"Bearer token"}`,
			want: map[string]string{"X-Trace-Id": "abc123", "Authorization": "Bearer token"},
		},
		{
			name: "header-string",
			body: "GET / HTTP/2\r\n:authority: www.example.com\r\n:method: GET\r\nUser-Agent: Mozilla/5.0\r\nAccept: text/html\r\nCookie: a=1; b=2\r\n",
			want: map[string]string{"User-Agent": "Mozilla/5.0", "Accept": "text/html", "Cookie": "a=1; b=2"},
		},
		{
			name: "json-cookie-array",
			body: `[{"domain":".reddit.com","hostOnly":false,"httpOnly":true,"name":"token_v2","path":"/","secure":true,"session":false,"value":"jwt.value"},{"domain":"www.reddit.com","hostOnly":true,"httpOnly":false,"name":"g_state","path":"/","secure":false,"session":false,"value":"{\"i_l\":0}"},{"domain":".reddit.com","hostOnly":false,"httpOnly":false,"name":"csv","path":"/","secure":true,"session":false,"value":"2"}]`,
			want: map[string]string{"Cookie": `token_v2=jwt.value; g_state={"i_l":0}; csv=2`},
		},
		{
			name: "json-header-array",
			body: `{"headers":[{"name":"User-Agent","value":"Mozilla/5.0"},{"name":"Accept","value":"text/html"}]}`,
			want: map[string]string{"User-Agent": "Mozilla/5.0", "Accept": "text/html"},
		},
		{
			name: "json-cookies-field",
			body: `{"cookies":[{"name":"a","value":"1"},{"name":"b","value":"2"}]}`,
			want: map[string]string{"Cookie": "a=1; b=2"},
		},
		{
			name: "netscape",
			body: "# Netscape HTTP Cookie File\n.example.com\tTRUE\t/\tFALSE\t2147483647\ta\t1\n#HttpOnly_.example.com\tTRUE\t/\tTRUE\t2147483647\tb\t2\n",
			want: map[string]string{"Cookie": "a=1; b=2"},
		},
		{
			name: "cookie-value",
			body: "a=1; b=2",
			want: map[string]string{"Cookie": "a=1; b=2"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(apiKeyEnv, "test-key")
			setMockHTTPClient(t, func(r *http.Request) (*http.Response, error) {
				var payload map[string]any
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}
				headers := payload["headers"].(map[string]any)
				for key, value := range tt.want {
					if headers[key] != value {
						t.Fatalf("headers[%q] = %#v, want %q; headers=%#v", key, headers[key], value, headers)
					}
				}
				return jsonResponse(200, `{"success":true,"data":{"markdown":"ok","metadata":{"url":"https://example.com"}}}`), nil
			})

			old := endpoints["scrape"]
			endpoints["scrape"] = "https://example.test/scrape"
			t.Cleanup(func() { endpoints["scrape"] = old })

			dir := t.TempDir()
			headersPath := filepath.Join(dir, "headers.txt")
			if err := os.WriteFile(headersPath, []byte(tt.body), 0o600); err != nil {
				t.Fatal(err)
			}

			var stdout, stderr bytes.Buffer
			err := run([]string{"scrape", "--output", "page", "--path", dir, "--url", "https://example.com", "--headers-file", headersPath}, &stdout, &stderr)
			if err != nil {
				t.Fatalf("run returned error: %v; stderr=%s", err, stderr.String())
			}
		})
	}
}

func TestScrapeCommandMergesHeadersFileWithHeadersFlag(t *testing.T) {
	t.Setenv(apiKeyEnv, "test-key")
	setMockHTTPClient(t, func(r *http.Request) (*http.Response, error) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		headers := payload["headers"].(map[string]any)
		if headers["Cookie"] != "a=1" || headers["X-Trace-Id"] != "override" {
			t.Fatalf("headers = %#v", headers)
		}
		return jsonResponse(200, `{"success":true,"data":{"markdown":"ok","metadata":{"url":"https://example.com"}}}`), nil
	})

	old := endpoints["scrape"]
	endpoints["scrape"] = "https://example.test/scrape"
	t.Cleanup(func() { endpoints["scrape"] = old })

	dir := t.TempDir()
	headersPath := filepath.Join(dir, "headers.txt")
	if err := os.WriteFile(headersPath, []byte("Cookie: a=1\nX-Trace-Id: from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	err := run([]string{
		"scrape",
		"--output", "page",
		"--path", dir,
		"--url", "https://example.com",
		"--headers-file", headersPath,
		"--headers", `{"X-Trace-Id":"override"}`,
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run returned error: %v; stderr=%s", err, stderr.String())
	}
}

func TestScrapeCommandSkipTLSFlag(t *testing.T) {
	t.Setenv(apiKeyEnv, "test-key")
	setMockHTTPClient(t, func(r *http.Request) (*http.Response, error) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["skipTlsVerification"] != true {
			t.Fatalf("skipTlsVerification = %#v", payload["skipTlsVerification"])
		}
		return jsonResponse(200, `{"success":true,"data":{"markdown":"ok","metadata":{"title":"T"}}}`), nil
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
		"--skip-tls",
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run returned error: %v; stderr=%s", err, stderr.String())
	}
}

func TestScrapeCommandScrollFlag(t *testing.T) {
	t.Setenv(apiKeyEnv, "test-key")
	setMockHTTPClient(t, func(r *http.Request) (*http.Response, error) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		actions := payload["actions"].([]any)
		if len(actions) != 2 {
			t.Fatalf("actions = %#v", actions)
		}
		waitAction := actions[0].(map[string]any)
		if waitAction["type"] != "wait" || waitAction["milliseconds"] != float64(2) {
			t.Fatalf("wait action = %#v", waitAction)
		}
		scrollAction := actions[1].(map[string]any)
		if scrollAction["type"] != "scroll" || scrollAction["direction"] != "down" || scrollAction["selector"] != "body" {
			t.Fatalf("scroll action = %#v", scrollAction)
		}
		return jsonResponse(200, `{"success":true,"data":{"markdown":"ok","metadata":{"title":"T"}}}`), nil
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
		"--scroll",
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run returned error: %v; stderr=%s", err, stderr.String())
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
	payload := buildScrapePayload("https://example.com", nil, []string{".nav", "script", ".nav"}, true, nil, defaultTimeoutSecs, false, false)
	excludeTags := payload["excludeTags"].([]string)
	if strings.Join(excludeTags, ",") != ".nav,script" {
		t.Fatalf("excludeTags = %#v", excludeTags)
	}
	if payload["timeout"] != int64(120000) {
		t.Fatalf("timeout = %#v", payload["timeout"])
	}
	if _, ok := payload["actions"]; ok {
		t.Fatalf("actions should be omitted by default: %#v", payload["actions"])
	}

	payloadWithScroll := buildScrapePayload("https://example.com", nil, []string{".nav", "script", ".nav"}, true, nil, defaultTimeoutSecs, false, true)
	actions := payloadWithScroll["actions"].([]map[string]any)
	if len(actions) != 2 {
		t.Fatalf("actions = %#v", actions)
	}
	if actions[0]["type"] != "wait" || actions[0]["milliseconds"] != 2 {
		t.Fatalf("wait action = %#v", actions[0])
	}
	if actions[1]["type"] != "scroll" || actions[1]["direction"] != "down" || actions[1]["selector"] != "body" {
		t.Fatalf("scroll action = %#v", actions[1])
	}

	payload = buildScrapePayload("https://example.com", nil, nil, true, nil, defaultTimeoutSecs, false, false)
	excludeTags = payload["excludeTags"].([]string)
	if len(excludeTags) != 0 {
		t.Fatalf("excludeTags = %#v", excludeTags)
	}
	if _, ok := payload["actions"]; ok {
		t.Fatalf("actions should be omitted with scroll=false: %#v", payload["actions"])
	}

	payload = buildScrapePayload("https://example.com", nil, []string{".nav"}, false, nil, defaultTimeoutSecs, false, false)
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
	dir := t.TempDir()
	headersPath := filepath.Join(dir, "headers.txt")
	if err := os.WriteFile(headersPath, []byte("not a standard headers file"), 0o600); err != nil {
		t.Fatal(err)
	}
	err = run([]string{"scrape", "--output", "x", "--url", "https://example.com", "--headers-file", headersPath}, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "--headers-file") {
		t.Fatalf("expected headers-file validation error, got %v", err)
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
