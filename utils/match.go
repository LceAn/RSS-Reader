package utils

import (
	"strings"
)

func Matches(text string, keywords []string) bool {
	normalizedText := strings.ToLower(strings.TrimSpace(text))
	for _, keyword := range keywords {
		normalizedKeyword := strings.ToLower(strings.TrimSpace(keyword))
		if normalizedKeyword != "" && strings.Contains(normalizedText, normalizedKeyword) {
			return true
		}
	}
	return false
}
