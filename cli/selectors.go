package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

func parseSelectorList(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if strings.HasPrefix(raw, "[") {
		var items []string
		if err := json.Unmarshal([]byte(raw), &items); err != nil {
			return nil, fmt.Errorf("must be a comma-separated string or JSON string array: %w", err)
		}
		return stableUnique(cleanStrings(items)), nil
	}
	parts := strings.Split(raw, ",")
	return stableUnique(cleanStrings(parts)), nil
}

func cleanStrings(items []string) []string {
	cleaned := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item != "" {
			cleaned = append(cleaned, item)
		}
	}
	return cleaned
}

func stableUnique(items []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(items))
	for _, item := range items {
		if seen[item] {
			continue
		}
		seen[item] = true
		result = append(result, item)
	}
	return result
}

func defaultScrapeExcludeTags() []string {
	return []string{
		"script", "style", "noscript", "form", "input", "button", "select", "textarea",
		"nav", ".nav", ".navbar", ".navigation", ".menu", ".menubar", "header", "footer", "aside",
		`[class*="logo"]`, `[class*="brand"]`, `[id*="logo"]`, `img[alt*="logo"]`, `img[src*="logo"]`,
		`[id*="brand"]`, `[class^="category--"]`, `[id^="category--"]`, `[class$="--category"]`, `[id$="--category"]`,
		`[class^="skip"]`, `[class*="accessib"]`, ".sr-only", ".visually-hidden",
		`[class*="-ad-"]`, `[class*="_ad_"]`, `[class$="-ad"]`, `[class$="_ad"]`, `[class^="ad-"]`,
		`[class^="ad_"]`, `[class*="advert"]`, ".sidebar", "#sidebar", `[class*="sidebar"]`,
		`[class*="sider"]`, `[class^="menu-"]`, `[class^="menu_"]`, `[class$="_module"]`,
		`[class$="-module"]`, `[class*="breadcrumb"]`, `[class*="pagination"]`, `[class*="relate"]`,
		`[class*="recommend"]`, `[class*="trending"]`, `[class^="header-"]`, `[class$="-header"]`,
		`[class^="header_"]`, `[class$="_header"]`, `[class*="footer"]`, `[class$="-offset"]`,
		`[class*="-offset-"]`, `[class$="_offset"]`, `[class*="_offset_"]`,
	}
}
