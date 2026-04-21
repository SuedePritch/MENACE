package tui

import (
	"strings"
	"unicode/utf8"
)

func stripAnsi(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inEsc := false
	for _, r := range s {
		if r == '\033' {
			inEsc = true
			continue
		}
		if inEsc {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func wordWrap(s string, w int) []string {
	if w < 10 {
		w = 10
	}
	var result []string
	for _, para := range strings.Split(s, "\n") {
		if utf8.RuneCountInString(para) <= w {
			result = append(result, para)
			continue
		}
		line := ""
		for _, word := range strings.Fields(para) {
			if utf8.RuneCountInString(line)+utf8.RuneCountInString(word)+1 > w {
				if line != "" {
					result = append(result, line)
				}
				line = word
			} else if line == "" {
				line = word
			} else {
				line += " " + word
			}
		}
		if line != "" {
			result = append(result, line)
		}
	}
	if len(result) == 0 {
		return []string{""}
	}
	return result
}

func visualLineCount(value string, width int) int {
	if width < 1 {
		width = 1
	}

	if value == "" {
		return 1
	}

	lines := strings.Split(value, "\n")
	var totalVisualLines int

	for _, line := range lines {
		if line == "" {
			totalVisualLines++
			continue
		}
		// Use word wrapping to calculate visual lines for this hard line
		wrapped := wordWrap(line, width)
		totalVisualLines += len(wrapped)
	}

	if totalVisualLines == 0 {
		return 1
	}

	return totalVisualLines
}

func truncate(s string, w int) string {
	runes := []rune(s)
	if len(runes) <= w {
		return s
	}
	if w < 4 {
		return string(runes[:w])
	}
	return string(runes[:w-1]) + "…"
}

