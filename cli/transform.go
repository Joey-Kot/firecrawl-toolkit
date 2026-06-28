package main

import (
	"net/url"
	"strings"
)

func transformSearchResult(raw map[string]any, data map[string]any) map[string]any {
	return map[string]any{
		"success": raw["success"],
		"data": map[string]any{
			"web":    mapItems(data["web"], []string{"title", "description", "url"}),
			"news":   mapItems(data["news"], []string{"title", "snippet", "url", "date"}),
			"images": mapItems(data["images"], []string{"title", "imageUrl", "url"}),
		},
		"creditsUsed": valueOrNil(raw, "creditsUsed"),
	}
}

func transformScholarResult(raw map[string]any) map[string]any {
	return map[string]any{
		"success": raw["success"],
		"data": map[string]any{
			"scholar": mapItems(raw["results"], []string{"title", "abstract", "paperId", "primaryId", "score"}),
		},
	}
}

func transformScrapeResult(raw map[string]any, data map[string]any, targetURL string) map[string]any {
	metadata, _ := data["metadata"].(map[string]any)
	markdown, _ := data["markdown"].(string)

	return map[string]any{
		"success":     raw["success"],
		"proxyUsed":   valueOrNil(metadata, "proxyUsed"),
		"title":       valueOrNil(metadata, "title"),
		"description": valueOrNil(metadata, "description"),
		"url":         scrapeURL(metadata, targetURL),
		"language":    valueOrNil(metadata, "language"),
		"markdown":    decodeMarkdownContent(markdown),
		"creditsUsed": valueOrNil(metadata, "creditsUsed"),
	}
}

func transformParseResult(raw map[string]any, data map[string]any, targetURL string) map[string]any {
	metadata, _ := data["metadata"].(map[string]any)
	markdown, _ := data["markdown"].(string)
	creditsUsed := valueOrNil(metadata, "creditsUsed")
	if creditsUsed == nil {
		creditsUsed = valueOrNil(raw, "creditsUsed")
	}
	return map[string]any{
		"title":       valueOrNil(metadata, "title"),
		"url":         scrapeURL(metadata, targetURL),
		"language":    valueOrNil(metadata, "language"),
		"creditsUsed": creditsUsed,
		"markdown":    decodeMarkdownContent(markdown),
	}
}

func decodeMarkdownContent(markdown string) string {
	decodedMarkdown := markdown
	if decoded, err := url.PathUnescape(markdown); err == nil {
		decodedMarkdown = decoded
	}
	return strings.ReplaceAll(decodedMarkdown, `\n`, "\n")
}

type audioScrapeResult struct {
	CreditsUsed any `json:"creditsUsed"`
	Title       any `json:"title"`
	Description any `json:"description"`
	Audio       any `json:"audio"`
	Success     any `json:"success"`
}

type videoScrapeResult struct {
	CreditsUsed any `json:"creditsUsed"`
	Title       any `json:"title"`
	Description any `json:"description"`
	Video       any `json:"video"`
	Success     any `json:"success"`
}

func transformAVScrapeResult(raw map[string]any, data map[string]any, format string) any {
	metadata, _ := data["metadata"].(map[string]any)
	creditsUsed := valueOrNil(metadata, "creditsUsed")
	if creditsUsed == nil {
		creditsUsed = valueOrNil(raw, "creditsUsed")
	}
	if format == "video" {
		return videoScrapeResult{
			CreditsUsed: creditsUsed,
			Title:       valueOrNil(metadata, "title"),
			Description: valueOrNil(metadata, "description"),
			Video:       valueOrNil(data, "video"),
			Success:     valueOrNil(raw, "success"),
		}
	}
	return audioScrapeResult{
		CreditsUsed: creditsUsed,
		Title:       valueOrNil(metadata, "title"),
		Description: valueOrNil(metadata, "description"),
		Audio:       valueOrNil(data, "audio"),
		Success:     valueOrNil(raw, "success"),
	}
}

func scrapeURL(metadata map[string]any, targetURL string) string {
	for _, key := range []string{"url", "sourceURL", "ogUrl"} {
		if val, ok := valueOrNil(metadata, key).(string); ok && strings.TrimSpace(val) != "" {
			return val
		}
	}
	return targetURL
}

func scrapeData(raw map[string]any) map[string]any {
	data, _ := raw["data"].(map[string]any)
	return data
}

func hasEmptyScrapeMarkdown(raw map[string]any) bool {
	data := scrapeData(raw)
	if data == nil {
		return false
	}
	markdown, ok := data["markdown"].(string)
	return ok && markdown == ""
}

func upstreamSuccessNotFalse(raw map[string]any) bool {
	success, ok := raw["success"].(bool)
	return !ok || success
}

func clonePayload(payload map[string]any) map[string]any {
	clone := make(map[string]any, len(payload))
	for key, value := range payload {
		clone[key] = value
	}
	return clone
}

func mapItems(items any, fields []string) []map[string]any {
	rawItems, ok := items.([]any)
	if !ok {
		return []map[string]any{}
	}
	mapped := make([]map[string]any, 0, len(rawItems))
	for _, item := range rawItems {
		row := make(map[string]any, len(fields))
		obj, _ := item.(map[string]any)
		for _, field := range fields {
			if obj == nil {
				row[field] = nil
				continue
			}
			row[field] = valueOrNil(obj, field)
		}
		mapped = append(mapped, row)
	}
	return mapped
}

func valueOrNil(obj map[string]any, key string) any {
	if obj == nil {
		return nil
	}
	if val, ok := obj[key]; ok {
		return val
	}
	return nil
}

func scrapeErrorReason(raw map[string]any) string {
	for _, key := range []string{"message", "error"} {
		if val, ok := raw[key]; ok && val != nil {
			return stringValue(val)
		}
	}
	if upstream, ok := raw["upstream"].(map[string]any); ok {
		return scrapeErrorReason(upstream)
	}
	return "unknown error"
}
