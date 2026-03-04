package webhook

import (
	"regexp"
	"strings"
)

func formatTelegramHTML(md string) string {
	// First escape <, >, & so they don't break HTML parsing
	md = strings.ReplaceAll(md, "&", "&amp;")
	md = strings.ReplaceAll(md, "<", "&lt;")
	md = strings.ReplaceAll(md, ">", "&gt;")

	// Markdown to HTML conversions
	// **bold**
	reBold := regexp.MustCompile(`(?s)\*\*(.*?)\*\*`)
	md = reBold.ReplaceAllString(md, "<b>$1</b>")

	// *italic* - Jules uses * for iterlic if not double **
	reItalic := regexp.MustCompile(`(?s)\*(.*?)\*`)
	md = reItalic.ReplaceAllString(md, "<i>$1</i>")

	// ```code block```
	reCodeBlock := regexp.MustCompile(`(?s)\x60\x60\x60(.*?)\x60\x60\x60`)
	md = reCodeBlock.ReplaceAllString(md, "<pre>$1</pre>")

	// `inline code`
	reInline := regexp.MustCompile(`(?s)\x60(.*?)\x60`)
	md = reInline.ReplaceAllString(md, "<code>$1</code>")

	return md
}
