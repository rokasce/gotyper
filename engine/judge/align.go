package judge

import "unicode/utf8"

// matchLines lines up got (the buffer) with want (the goal) by their longest
// common subsequence: the longest list of lines that appear, in the same
// order, in both. It returns, for each row of got, the index of the want row
// it is matched with, or -1 when that row is not part of the common lines.
//
// Why this is needed: a drill changes the shape of the file. When the
// learner inserts a line, every line below it moves down one row. Comparing
// row n with row n, as type-along does, would then call all of those lines
// wrong although they are untouched. Matching by common subsequence pairs
// each untouched line with its twin wherever it now sits, so only the lines
// that really differ are left unmatched.
//
// It is the classic dynamic-programming solution. lcs[i][j] is the length
// of the longest common subsequence of got[i:] and want[j:]. It is filled
// from the bottom-right corner up: if got[i] and want[j] are equal, they can
// both be kept, giving 1 + lcs[i+1][j+1]; otherwise one of the two lines is
// dropped, whichever leaves the longer subsequence. Then a walk from the
// top-left corner follows those choices to read the matches off. Drills are
// a screenful of code, so the len(got) x len(want) table is tiny.
func matchLines(got, want []string) []int {
	lcs := make([][]int, len(got)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(want)+1)
	}
	for i := len(got) - 1; i >= 0; i-- {
		for j := len(want) - 1; j >= 0; j-- {
			if got[i] == want[j] {
				lcs[i][j] = 1 + lcs[i+1][j+1]
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	match := make([]int, len(got))
	i, j := 0, 0
	for i < len(got) && j < len(want) {
		switch {
		case got[i] == want[j]:
			match[i] = j
			i, j = i+1, j+1
		case lcs[i+1][j] >= lcs[i][j+1]:
			match[i] = -1 // dropping this buffer row keeps the longest match
			i++
		default:
			j++ // this goal row is missing from the buffer
		}
	}
	for ; i < len(got); i++ {
		match[i] = -1
	}
	return match
}

// diffRun returns the byte range [col, endCol) of got that differs from
// want, found by skipping the runes the two lines share at the start and at
// the end. So "total := 0" against "t := 0" marks only "otal", not the whole
// line. When got is want with something deleted, nothing of got differs, so
// the rune at the deletion point is marked instead (or the rune before it at
// the end of the line), to show where the text is missing. ok is false when
// got is empty and there is nothing to mark.
func diffRun(got, want string) (col, endCol int, ok bool) {
	if got == "" {
		return 0, 0, false
	}
	// The common prefix, rune by rune.
	p := 0
	for p < len(got) && p < len(want) {
		g, size := utf8.DecodeRuneInString(got[p:])
		w, _ := utf8.DecodeRuneInString(want[p:])
		if g != w {
			break
		}
		p += size
	}
	// The common suffix, rune by rune, without eating into the prefix of
	// either line.
	gEnd, wEnd := len(got), len(want)
	for gEnd > p && wEnd > p {
		g, size := utf8.DecodeLastRuneInString(got[:gEnd])
		w, _ := utf8.DecodeLastRuneInString(want[:wEnd])
		if g != w {
			break
		}
		gEnd -= size
		wEnd -= size
	}
	if gEnd > p {
		return p, gEnd, true
	}
	// Only deleted text: mark one rune next to where it is missing.
	if p < len(got) {
		_, size := utf8.DecodeRuneInString(got[p:])
		return p, p + size, true
	}
	_, size := utf8.DecodeLastRuneInString(got)
	return len(got) - size, len(got), true
}
