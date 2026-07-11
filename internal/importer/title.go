package importer

import "strings"

// leadingDashRunes are the specific dash characters seen in source
// captions: hyphen-minus, em dash, en dash.
var leadingDashRunes = []rune{'-', '—', '–'}

// extract raw caption and return the first line, trimmed of whitespace and leading dashes.
func ExtractCleanTitle(caption string) string {
	if caption == "" {
		return ""
	}

	firstLine := caption
	if idx := strings.IndexAny(caption, "\r\n"); idx != -1 {
		firstLine = caption[:idx]
	}

	firstLine = strings.TrimSpace(firstLine)
	firstLine = strings.TrimLeftFunc(firstLine, func(r rune) bool {
		for _, d := range leadingDashRunes {
			if r == d {
				return true
			}
		}
		return false
	})

	return strings.TrimSpace(firstLine)
}
