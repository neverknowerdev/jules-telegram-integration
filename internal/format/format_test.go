package format

import (
	"testing"
)

func TestEscapeHTML(t *testing.T) {
	input := "x < y & z > w"
	expected := "x &lt; y &amp; z &gt; w"
	if out := EscapeHTML(input); out != expected {
		t.Errorf("expected %q, got %q", expected, out)
	}
}

func TestTelegramHTML(t *testing.T) {
	input := "**bold** *italic* `code`"
	expected := "<b>bold</b> <i>italic</i> <code>code</code>"
	if out := TelegramHTML(input); out != expected {
		t.Errorf("expected %q, got %q", expected, out)
	}
}
