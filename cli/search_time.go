package main

import (
	"fmt"
	"strings"
)

func mapSearchTime(value string) (string, error) {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "":
		return "", nil
	case "hour":
		return "qdr:h", nil
	case "day":
		return "qdr:d", nil
	case "week":
		return "qdr:w", nil
	case "month":
		return "qdr:m", nil
	case "year":
		return "qdr:y", nil
	default:
		return "", fmt.Errorf(`--search-time must be one of "hour", "day", "week", "month", "year"`)
	}
}
