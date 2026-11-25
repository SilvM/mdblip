package main

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// Private-use sentinels that Glamour's heading prefix carries, one per
// level, so headings can be found in the rendered lines.
const headBase = '\ue020'

type heading struct {
	line  int // index into the full rendered document
	level int
	key   string // identity that survives reloads
}

func isHeadMark(r rune) bool { return r > headBase && r <= headBase+6 }

// takeHeadings strips the heading sentinels from rendered lines and returns
// where the headings are.
func takeHeadings(lines []string) ([]string, []heading) {
	var heads []heading
	seen := map[string]int{}
	for i, l := range lines {
		at := strings.IndexFunc(l, isHeadMark)
		if at < 0 {
			continue
		}
		mark, size := utf8.DecodeRuneInString(l[at:])
		lines[i] = l[:at] + l[at+size:]
		text := strings.TrimSpace(ansi.Strip(lines[i]))
		seen[text]++
		heads = append(heads, heading{i, int(mark - headBase), fmt.Sprintf("%s#%d", text, seen[text])})
	}
	return lines, heads
}

// sectionEnd returns the line after the last line of a heading's section:
// the next heading of the same or a higher level, or the end.
func (m *model) sectionEnd(h int) int {
	for _, next := range m.heads[h+1:] {
		if next.level <= m.heads[h].level {
			return next.line
		}
	}
	return len(m.all)
}

// applyFolds rebuilds the visible lines from the full document, leaving out
// the sections of folded headings.
func (m *model) applyFolds() {
	m.lines, m.vis, m.foldMark = nil, nil, map[int]bool{}
	hiddenUntil, h := 0, 0
	for i, l := range m.all {
		for h < len(m.heads) && m.heads[h].line < i {
			h++
		}
		if i < hiddenUntil {
			continue
		}
		if h < len(m.heads) && m.heads[h].line == i && m.folded[m.heads[h].key] {
			hiddenUntil = m.sectionEnd(h)
			text := ansi.Truncate(l, ansi.StringWidth(strings.TrimRight(ansi.Strip(l), " ")), "")
			l = fmt.Sprintf("%s%s ⋯ %d lines\x1b[m", text, dim, hiddenUntil-i-1)
			m.foldMark[len(m.lines)] = true
		}
		m.lines = append(m.lines, l)
		m.vis = append(m.vis, i)
	}
}

// visible returns the on-screen line index of a full-document line, or -1
// if it is inside a fold.
func (m *model) visible(line int) int {
	if i, ok := slices.BinarySearch(m.vis, line); ok {
		return i
	}
	return -1
}

// currentHead picks the heading a fold key applies to: the first one on
// screen, or failing that the one whose section the top line is in.
func (m *model) currentHead() int {
	above := -1
	for h, head := range m.heads {
		v := m.visible(head.line)
		switch {
		case v < 0:
		case v < m.offset:
			above = h
		case v < m.offset+m.page():
			return h
		}
	}
	return above
}

func (m *model) refold() {
	m.applyFolds()
	m.blips = nil
	m.scroll(0)
	m.hscroll(0)
	m.search()
}

func (m *model) toggleFold(h int) {
	if h < 0 {
		return
	}
	if m.folded == nil {
		m.folded = map[string]bool{}
	}
	key := m.heads[h].key
	m.folded[key] = !m.folded[key]
	m.refold()
}

// toggleAllFolds folds every section below the document's top heading level,
// or opens them all if all are folded,
// keeping the current heading (or the folded heading above it) in view.
func (m *model) toggleAllFolds() {
	cur := m.currentHead()
	if cur < 0 {
		return
	}
	fold := false
	top := 6
	for _, head := range m.heads {
		top = min(top, head.level)
	}
	for _, head := range m.heads {
		fold = fold || head.level > top && !m.folded[head.key]
	}
	m.folded = map[string]bool{}
	for _, head := range m.heads {
		m.folded[head.key] = fold && head.level > top
	}
	m.applyFolds()
	for ; cur >= 0; cur-- {
		if v := m.visible(m.heads[cur].line); v >= 0 {
			m.offset = v
			break
		}
	}
	m.refold()
}

// unfoldChanged opens any folded section that contains a changed line, so
// an edit saved inside a fold is not missed.
func (m *model) unfoldChanged(diff map[int]bool) {
	for h, head := range m.heads {
		if !m.folded[head.key] {
			continue
		}
		end := m.sectionEnd(h)
		for line := head.line + 1; line < end; line++ {
			if diff[line] {
				delete(m.folded, head.key)
				break
			}
		}
	}
}

// headAt returns the heading shown on the given on-screen line, or -1.
func (m *model) headAt(visLine int) int {
	if visLine < 0 || visLine >= len(m.vis) {
		return -1
	}
	return slices.IndexFunc(m.heads, func(h heading) bool { return h.line == m.vis[visLine] })
}
