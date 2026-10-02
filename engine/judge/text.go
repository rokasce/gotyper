package judge

import "strings"

// tabWidth is how many columns a tab advances to, matching the front end's
// tabstop for the game buffer.
const tabWidth = 4

// leadingWhitespace returns the indentation of line, given the same line with
// its indentation already stripped.
func leadingWhitespace(line, stripped string) string {
	return line[:len(line)-len(stripped)]
}

// displayWidth returns how many screen columns the whitespace ws occupies
// when it starts at column 0.
func displayWidth(ws string) int {
	w := 0
	for _, c := range ws {
		if c == '\t' {
			w += tabWidth - w%tabWidth
		} else {
			w++
		}
	}
	return w
}

// expandTabs replaces each tab in s with the spaces it would occupy on
// screen, given that s starts at display column startCol. Virtual text in
// Neovim does not expand tabs itself, so ghosts must arrive pre-expanded.
func expandTabs(s string, startCol int) string {
	var b strings.Builder
	col := startCol
	for _, c := range s {
		if c == '\t' {
			n := tabWidth - col%tabWidth
			b.WriteString(strings.Repeat(" ", n))
			col += n
			continue
		}
		b.WriteRune(c)
		col++
	}
	return b.String()
}
