package report

import (
	"strings"
	"unicode"
)

// terminalValue converts one untrusted field into a single printable line.
// Removing ESC/C0/C1 controls prevents terminal-state manipulation; collapsing
// whitespace prevents a repository value from forging additional report rows.
func terminalValue(value string) string {
	value = strings.Map(func(character rune) rune {
		if unicode.IsControl(character) || unicode.In(character, unicode.Cf) {
			return ' '
		}
		return character
	}, value)
	return strings.Join(strings.FieldsFunc(value, unicode.IsSpace), " ")
}

// markdownCode renders untrusted text as a single safe CommonMark code span.
// The fence is longer than every backtick run in the value, so repository data
// cannot terminate the span, create links/images, or trigger user mentions.
func markdownCode(value string) string {
	value = terminalValue(value)
	if value == "" {
		return "``"
	}
	longest := 0
	current := 0
	for _, character := range value {
		if character == '`' {
			current++
			if current > longest {
				longest = current
			}
			continue
		}
		current = 0
	}
	fence := strings.Repeat("`", longest+1)
	return fence + " " + value + " " + fence
}
