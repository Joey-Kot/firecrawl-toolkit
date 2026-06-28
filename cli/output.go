package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func compactJSON(payload any) string {
	return formatJSON(payload, false)
}

func formatJSON(payload any, pretty bool) string {
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	if pretty {
		encoder.SetIndent("", "  ")
	}
	if err := encoder.Encode(payload); err != nil {
		return `{"success":false,"error":true,"message":"failed to encode JSON"}`
	}
	return strings.TrimSuffix(out.String(), "\n")
}

func renderMarkdownFile(result map[string]any) string {
	return fmt.Sprintf("## title: %s\n## description: %s\n## url: %s\n## language: %s\n## creditsUsed: %s\n\n---\n\n%s\n",
		stringValue(result["title"]),
		stringValue(result["description"]),
		stringValue(result["url"]),
		stringValue(result["language"]),
		stringValue(result["creditsUsed"]),
		stringValue(result["markdown"]),
	)
}

func renderParseMarkdownFile(result map[string]any) string {
	return fmt.Sprintf("## title: %s\n## url: %s\n## language: %s\n## creditsUsed: %s\n\n%s\n",
		stringValue(result["title"]),
		stringValue(result["url"]),
		stringValue(result["language"]),
		stringValue(result["creditsUsed"]),
		stringValue(result["markdown"]),
	)
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	switch v := value.(type) {
	case string:
		return v
	default:
		return fmt.Sprint(v)
	}
}

func outputPath(output string, outputDir string) string {
	name := filepath.Base(strings.TrimSpace(output))
	if !strings.HasSuffix(strings.ToLower(name), ".md") {
		name += ".md"
	}
	dir := strings.TrimSpace(outputDir)
	if dir == "" {
		dir = "."
	}
	return filepath.Join(dir, name)
}

func ensureOutputDir(outputDir string) error {
	dir := strings.TrimSpace(outputDir)
	if dir == "" {
		dir = "."
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create output path %q: %w", dir, err)
	}
	return nil
}
