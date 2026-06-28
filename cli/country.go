package main

import (
	"encoding/json"
	"golang.org/x/text/unicode/norm"
	"regexp"
	"strings"
	"unicode"
)

func loadCountryAliases() map[string]string {
	data, err := embeddedData.ReadFile("data/country_aliases.json")
	if err != nil {
		return map[string]string{}
	}
	var raw map[string][]string
	if err := json.Unmarshal(data, &raw); err != nil {
		return map[string]string{}
	}
	aliases := map[string]string{}
	for code, names := range raw {
		code = strings.ToUpper(code)
		for _, name := range names {
			for _, variant := range countryVariants(name) {
				key := normalizeCountry(variant)
				if key != "" {
					aliases[key] = code
				}
			}
		}
	}
	return aliases
}

func getCountryCodeAlpha2(country string) string {
	name := strings.TrimSpace(country)
	if name == "" {
		return "US"
	}
	norm := normalizeCountry(name)
	if code, ok := countryAliases[norm]; ok {
		return code
	}
	if len([]rune(name)) == 2 && isLetters(name) {
		return strings.ToUpper(name)
	}
	if code, ok := countryAliases[normalizeCountry(strings.ToUpper(name))]; ok {
		return code
	}
	return "US"
}

func countryVariants(alias string) []string {
	base := strings.TrimSpace(alias)
	if base == "" {
		return nil
	}
	variants := []string{base, normalizeCountry(base), stripCountryPunctuation(normalizeCountry(base))}
	if strings.Contains(base, ",") {
		parts := strings.Split(base, ",")
		cleaned := make([]string, 0, len(parts))
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part != "" {
				cleaned = append(cleaned, part)
			}
		}
		if len(cleaned) >= 2 {
			for i, j := 0, len(cleaned)-1; i < j; i, j = i+1, j-1 {
				cleaned[i], cleaned[j] = cleaned[j], cleaned[i]
			}
			reordered := strings.Join(cleaned, " ")
			variants = append(variants, reordered, normalizeCountry(reordered))
		}
	}
	parts := strings.Fields(normalizeCountry(base))
	if len(parts) == 2 {
		variants = append(variants, parts[1]+" "+parts[0])
	}
	return stableUnique(variants)
}

func normalizeCountry(value string) string {
	value = norm.NFKD.String(value)
	var folded strings.Builder
	for _, r := range value {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		folded.WriteRune(r)
	}
	value = strings.ReplaceAll(folded.String(), "\u3000", " ")
	value = strings.TrimSpace(strings.ToLower(value))
	var b strings.Builder
	previousSpace := false
	for _, r := range value {
		switch {
		case r == '_' || unicode.IsSpace(r):
			if !previousSpace {
				b.WriteRune(' ')
				previousSpace = true
			}
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '\'' || r == '-':
			b.WriteRune(r)
			previousSpace = false
		default:
			if !previousSpace {
				b.WriteRune(' ')
				previousSpace = true
			}
		}
	}
	return strings.TrimSpace(b.String())
}

func stripCountryPunctuation(value string) string {
	re := regexp.MustCompile(`[^\p{L}\p{N}\s]`)
	return strings.TrimSpace(re.ReplaceAllString(value, ""))
}

func isLetters(value string) bool {
	for _, r := range value {
		if !unicode.IsLetter(r) {
			return false
		}
	}
	return true
}
