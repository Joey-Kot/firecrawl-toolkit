package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

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

func firecrawlGet(endpointName string, proxyRaw string) (map[string]any, error) {
	key, err := apiKeyFromEnv()
	if err != nil {
		return nil, err
	}
	client, err := clientWithProxy(proxyRaw)
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
	resp, err := client.Do(req)
	if err != nil {
		return nil, firecrawlRequestError{endpoint: endpointName, err: err}
	}
	defer resp.Body.Close()
	return parseFirecrawlResponse(endpointName, resp)
}

func firecrawlGetWithRetry(endpointName string, proxyRaw string) (map[string]any, error) {
	return firecrawlWithRetry(func() (map[string]any, error) {
		return firecrawlGet(endpointName, proxyRaw)
	})
}

func firecrawlGetWithJSONBody(endpointName string, query url.Values, payload map[string]any, timeoutSecs int, proxyRaw string) (map[string]any, error) {
	key, err := apiKeyFromEnv()
	if err != nil {
		return nil, err
	}
	endpoint := endpointURL(endpointName)
	if len(query) > 0 {
		separator := "?"
		if strings.Contains(endpoint, "?") {
			separator = "&"
		}
		endpoint += separator + query.Encode()
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodGet, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "firecrawl_cli/1.0")
	client, err := clientWithTimeout(timeoutSecs, proxyRaw)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, firecrawlRequestError{endpoint: endpointName, err: err}
	}
	defer resp.Body.Close()
	return parseFirecrawlResponse(endpointName, resp)
}

func firecrawlGetWithJSONBodyWithRetry(endpointName string, query url.Values, payload map[string]any, timeoutSecs int, proxyRaw string) (map[string]any, error) {
	return firecrawlWithRetry(func() (map[string]any, error) {
		return firecrawlGetWithJSONBody(endpointName, query, payload, timeoutSecs, proxyRaw)
	})
}

func firecrawlPost(endpointName string, payload map[string]any, timeoutSecs int, proxyRaw string) (map[string]any, error) {
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
	client, err := clientWithTimeout(timeoutSecs, proxyRaw)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, firecrawlRequestError{endpoint: endpointName, err: err}
	}
	defer resp.Body.Close()
	return parseFirecrawlResponse(endpointName, resp)
}

func firecrawlPostMultipartFile(endpointName string, filePath string, options map[string]any, timeoutSecs int, proxyRaw string) (map[string]any, error) {
	key, err := apiKeyFromEnv()
	if err != nil {
		return nil, err
	}
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filepath.Base(filePath))
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(part, file); err != nil {
		return nil, err
	}
	optionsJSON, err := json.Marshal(options)
	if err != nil {
		return nil, err
	}
	if err := writer.WriteField("options", string(optionsJSON)); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}

	endpoint := endpointURL(endpointName)
	req, err := http.NewRequest(http.MethodPost, endpoint, &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("User-Agent", "firecrawl_cli/1.0")
	client, err := clientWithTimeout(timeoutSecs, proxyRaw)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, firecrawlRequestError{endpoint: endpointName, err: err}
	}
	defer resp.Body.Close()
	return parseFirecrawlResponse(endpointName, resp)
}

func firecrawlPostWithRetry(endpointName string, payload map[string]any, timeoutSecs int, proxyRaw string) (map[string]any, error) {
	return firecrawlWithRetry(func() (map[string]any, error) {
		return firecrawlPost(endpointName, payload, timeoutSecs, proxyRaw)
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
	case "scholar":
		return joinEndpoint(baseURL, "search/research/papers")
	case "scrape":
		return joinEndpoint(baseURL, "scrape")
	case "parse":
		return joinEndpoint(baseURL, "parse")
	case "credit-usage":
		return joinEndpoint(baseURL, "team/credit-usage")
	default:
		return endpoints[endpointName]
	}
}

func joinEndpoint(baseURL string, path string) string {
	return strings.TrimRight(strings.TrimSpace(baseURL), "/") + "/" + strings.TrimLeft(path, "/")
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
