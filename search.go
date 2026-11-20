package main

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// fuzzy reports whether query's characters appear in line in order, within
// a span of at most twice the query length, ignoring case.
func fuzzy(query, line string) bool {
	q, l := []rune(strings.ToLower(query)), []rune(strings.ToLower(line))
	if len(q) == 0 {
		return false
	}
	for start, r := range l {
		if r != q[0] {
			continue
		}
		qi := 1
		for i := start + 1; i < len(l) && qi < len(q) && i-start < 2*len(q); i++ {
			if l[i] == q[qi] {
				qi++
			}
		}
		if qi == len(q) {
			return true
		}
	}
	return false
}

// search recomputes the matching lines and moves to the first one at or
// below the top of the screen.
func (m *model) search() {
	m.matches, m.matched, m.cur = nil, map[int]bool{}, 0
	for i, line := range m.lines {
		if fuzzy(m.query, ansi.Strip(line)) {
			if len(m.matches) == 0 || i < m.offset {
				m.cur = len(m.matches)
			}
			m.matches = append(m.matches, i)
			m.matched[i] = true
		}
	}
	if m.cur < len(m.matches)-1 && m.matches[m.cur] < m.offset {
		m.cur++
	}
	m.jump(0)
}

func (m *model) jump(delta int) {
	if len(m.matches) == 0 {
		return
	}
	m.cur = (m.cur + delta + len(m.matches)) % len(m.matches)
	if line := m.matches[m.cur]; line < m.offset || line >= m.offset+m.page() {
		m.offset = line - m.page()/3
		m.scroll(0)
	}
}

// typeKey handles a key press while the search prompt is open.
func (m *model) typeKey(key string) {
	switch {
	case key == "enter":
		m.searching = false
		return
	case key == "esc":
		m.searching, m.query = false, ""
	case key == "backspace":
		if r := []rune(m.query); len(r) > 0 {
			m.query = string(r[:len(r)-1])
		}
	case key == "space":
		m.query += " "
	case len([]rune(key)) == 1:
		m.query += key
	default:
		return
	}
	m.search()
}
