package main

import (
	"net/url"
	"os/exec"
	"regexp"
	"runtime"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Glamour wraps links in OSC 8 hyperlink sequences; an empty target closes one.
var hyperlinkRe = regexp.MustCompile(`\x1b\]8;[^;]*;([^\x1b\x07]*)(?:\x1b\\|\x07)`)

// linkAt returns the link target under the given column of a rendered line.
func linkAt(line string, col int) string {
	pos, prev, target := 0, 0, ""
	for _, loc := range hyperlinkRe.FindAllStringSubmatchIndex(line, -1) {
		width := ansi.StringWidth(line[prev:loc[0]])
		if target != "" && col >= pos && col < pos+width {
			return target
		}
		pos += width
		prev, target = loc[1], line[loc[2]:loc[3]]
	}
	return ""
}

// underline underlines every part of the line that links to target,
// re-applying it after each reset inside the link.
func underline(line, target string) string {
	var b strings.Builder
	prev, inside := 0, false
	for _, loc := range hyperlinkRe.FindAllStringSubmatchIndex(line, -1) {
		seg := line[prev:loc[0]]
		if inside {
			seg = "\x1b[4m" + resetRe.ReplaceAllString(seg, "\x1b[m\x1b[4m") + "\x1b[24m"
		}
		b.WriteString(seg + line[loc[0]:loc[1]])
		prev, inside = loc[1], line[loc[2]:loc[3]] == target
	}
	return b.String() + line[prev:]
}

var openURL = func(target string) error {
	opener := "xdg-open"
	if runtime.GOOS == "darwin" {
		opener = "open"
	}
	return exec.Command(opener, target).Start()
}

// linkUnder returns the link target at a screen position, if any.
func (m *model) linkUnder(x, y int) string {
	line := m.offset + y
	if m.help || y < 0 || y >= m.page() || line >= len(m.lines) {
		return ""
	}
	return linkAt(m.lines[line], x-gutter+m.xoff)
}

// click opens the web or mail link under the mouse, or failing that folds
// or opens the heading on that line.
func (m *model) click(x, y int) {
	target := m.linkUnder(x, y)
	if target == "" {
		if !m.help && y < m.page() {
			m.toggleFold(m.headAt(m.offset + y))
		}
		return
	}
	u, err := url.Parse(target)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "mailto") {
		m.notice = "not a web link: " + target
		return
	}
	if err := openURL(target); err != nil {
		m.notice = "open failed: " + err.Error()
		return
	}
	m.notice = "opened " + target
}
