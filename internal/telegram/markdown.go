package telegram

import (
	"regexp"
	"strings"
)

func EscapeMarkdown(text string) string {
	// Simple converter from GFM to Telegram Legacy Markdown
	// 1. Replace **bold** with *bold*
	text = strings.ReplaceAll(text, "**", "*")
	// 2. Replace __italic__ with _italic_
	text = strings.ReplaceAll(text, "__", "_")

	// 3. Headers: # Header -> *Header*
	// Regex for headers at start of line
	reHeader := regexp.MustCompile(`(?m)^#+\s+(.*)`)
	text = reHeader.ReplaceAllString(text, "*$1*")

	// 4. Tables: Not supported in Telegram. Convert to code block?
	// Crude check for | separator
	if strings.Contains(text, "|") && strings.Contains(text, "-") {
		// Maybe wrap whole table in code block?
		// Too complex to detect boundaries reliably with simple regex.
	}

	return text
}
