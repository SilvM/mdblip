package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const (
	gutter     = 2
	blipFrames = 8
	blipTick   = 50 * time.Millisecond
	maxRetries = 5
	hstep      = 8
)

type (
	reloadMsg struct{ try int }
	tickMsg   struct{}
)

type model struct {
	path          string
	src           string
	lines         []string
	offset, xoff  int
	width, height int
	wrap, help    bool
	searching     bool
	query         string
	matches       []int
	matched       map[int]bool
	cur           int
	notice        string
	hover         string
	blips         map[int]int
	ticking       bool
	updated       time.Time
	err           error
}

type binding struct {
	keys []string
	desc string
	run  func(*model) tea.Cmd
}

// bindings drives both key dispatch and the help menu: add a shortcut here
// and it shows up under `?`.
var bindings []binding

func init() {
	bindings = []binding{
		{[]string{"j", "down", "enter"}, "line down", func(m *model) tea.Cmd { m.scroll(1); return nil }},
		{[]string{"k", "up"}, "line up", func(m *model) tea.Cmd { m.scroll(-1); return nil }},
		{[]string{"d", "ctrl+d"}, "half page down", func(m *model) tea.Cmd { m.scroll(m.page() / 2); return nil }},
		{[]string{"u", "ctrl+u"}, "half page up", func(m *model) tea.Cmd { m.scroll(-m.page() / 2); return nil }},
		{[]string{"space", "f", "pgdown"}, "page down", func(m *model) tea.Cmd { m.scroll(m.page()); return nil }},
		{[]string{"b", "pgup"}, "page up", func(m *model) tea.Cmd { m.scroll(-m.page()); return nil }},
		{[]string{"g", "home"}, "top", func(m *model) tea.Cmd { m.offset = 0; return nil }},
		{[]string{"G", "end"}, "bottom", func(m *model) tea.Cmd { m.scroll(len(m.lines)); return nil }},
		{[]string{"}"}, "next paragraph", func(m *model) tea.Cmd { m.paragraph(1); return nil }},
		{[]string{"{"}, "previous paragraph", func(m *model) tea.Cmd { m.paragraph(-1); return nil }},
		{[]string{"w"}, "toggle line wrap", func(m *model) tea.Cmd { m.wrap = !m.wrap; m.rerender(); return nil }},
		{[]string{"l", "right"}, "scroll right (unwrapped)", func(m *model) tea.Cmd { m.hscroll(hstep); return nil }},
		{[]string{"h", "left"}, "scroll left (unwrapped)", func(m *model) tea.Cmd { m.hscroll(-hstep); return nil }},
		{[]string{"0"}, "back to first column", func(m *model) tea.Cmd { m.xoff = 0; return nil }},
		{[]string{"/"}, "fuzzy search (enter to keep, esc to clear)", func(m *model) tea.Cmd { m.searching, m.query = true, ""; m.search(); return nil }},
		{[]string{"n"}, "next match", func(m *model) tea.Cmd { m.jump(1); return nil }},
		{[]string{"N"}, "previous match", func(m *model) tea.Cmd { m.jump(-1); return nil }},
		{[]string{"?"}, "toggle this help", func(m *model) tea.Cmd { m.help = !m.help; return nil }},
		{[]string{"q", "ctrl+c"}, "quit", func(*model) tea.Cmd { return tea.Quit }},
	}
}

var (
	statusBar = lipgloss.NewStyle().Background(lipgloss.Color(bg1)).Foreground(lipgloss.Color(gray))
	errStyle  = lipgloss.NewStyle().Background(lipgloss.Color(bg1)).Foreground(lipgloss.Color(red))
	helpBox   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(bg3)).Padding(1, 3)
	helpTitle = lipgloss.NewStyle().Foreground(lipgloss.Color(yellow)).Bold(true)
	helpKey   = lipgloss.NewStyle().Foreground(lipgloss.Color(aqua))
	helpDesc  = lipgloss.NewStyle().Foreground(lipgloss.Color(fg))
)

func (m model) Init() tea.Cmd { return nil }

func (m model) page() int { return max(m.height-1, 1) }

func (m *model) scroll(delta int) {
	m.offset = max(min(m.offset+delta, len(m.lines)-m.page()), 0)
}

// paragraph moves the top of the screen to the next or previous blank line,
// like vim's } and {.
func (m *model) paragraph(dir int) {
	i := m.offset + dir
	for i > 0 && i < len(m.lines) && !m.blank(i) {
		i += dir
	}
	m.offset = i
	m.scroll(0)
}

func (m *model) blank(i int) bool {
	return !strings.Contains(m.lines[i], codeBg) && strings.TrimSpace(ansi.Strip(m.lines[i])) == ""
}

func (m *model) hscroll(delta int) {
	widest := 0
	for _, l := range m.lines {
		widest = max(widest, ansi.StringWidth(l))
	}
	m.xoff = max(min(m.xoff+delta, widest-(m.width-gutter)), 0)
}

func (m *model) rerender() {
	m.lines = render(m.src, m.width-gutter, m.wrap)
	m.blips = nil
	m.scroll(0)
	m.hscroll(0)
	m.search()
}

func tick() tea.Cmd {
	return tea.Tick(blipTick, func(time.Time) tea.Msg { return tickMsg{} })
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.rerender()
	case reloadMsg:
		return m.reload(msg)
	case tickMsg:
		for i := range m.blips {
			if m.blips[i]--; m.blips[i] <= 0 {
				delete(m.blips, i)
			}
		}
		if len(m.blips) > 0 {
			return m, tick()
		}
		m.ticking = false
	case tea.MouseWheelMsg:
		switch msg.Button {
		case tea.MouseWheelUp:
			m.scroll(-3)
		case tea.MouseWheelDown:
			m.scroll(3)
		}
	case tea.MouseMotionMsg:
		m.hover = m.linkUnder(msg.X, msg.Y)
	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft {
			m.click(msg.X, msg.Y)
		}
	case tea.KeyPressMsg:
		m.notice = ""
		return m.press(msg.String())
	}
	return m, nil
}

func (m model) press(key string) (model, tea.Cmd) {
	switch {
	case m.searching:
		m.typeKey(key)
		return m, nil
	case m.help && key == "esc":
		m.help = false
		return m, nil
	case key == "esc":
		m.query = ""
		m.search()
		return m, nil
	}
	for _, b := range bindings {
		for _, k := range b.keys {
			if k == key {
				return m, b.run(&m)
			}
		}
	}
	return m, nil
}

func (m model) reload(msg reloadMsg) (tea.Model, tea.Cmd) {
	data, err := os.ReadFile(m.path)
	// A save by rename briefly leaves the file missing or empty.
	if (err != nil || len(data) == 0) && msg.try < maxRetries {
		return m, tea.Tick(20*time.Millisecond, func(time.Time) tea.Msg { return reloadMsg{msg.try + 1} })
	}
	m.err = err
	if err != nil || string(data) == m.src {
		return m, nil
	}
	m.src = string(data)
	m.updated = time.Now()
	if m.width == 0 {
		return m, nil
	}
	lines := render(m.src, m.width-gutter, m.wrap)
	diff := changed(m.lines, lines)
	m.lines = lines
	m.blips = make(map[int]int, len(diff))
	first := -1
	for i := range diff {
		m.blips[i] = blipFrames
		if first < 0 || i < first {
			first = i
		}
	}
	if first >= 0 && (first < m.offset || first >= m.offset+m.page()) {
		m.offset = first - m.page()/3
	}
	m.scroll(0)
	m.hscroll(0)
	offset := m.offset
	m.search()
	m.offset = offset
	if len(m.blips) > 0 && !m.ticking {
		m.ticking = true
		return m, tick()
	}
	return m, nil
}

func lerp(from, to string, t float64) string {
	var out [3]int
	for i := range out {
		a, _ := strconv.ParseInt(from[1+2*i:3+2*i], 16, 0)
		b, _ := strconv.ParseInt(to[1+2*i:3+2*i], 16, 0)
		out[i] = int(float64(a) + (float64(b)-float64(a))*t)
	}
	return fmt.Sprintf("%d;%d;%d", out[0], out[1], out[2])
}

// mark draws a line with a coloured gutter bar and a background tint, each
// blended from the base background by the given strength.
func mark(line, color string, bar, bg float64, width int) string {
	out := "\x1b[38;2;" + lerp(bg0, color, bar) + "m▎\x1b[m"
	if bg == 0 {
		return out + " " + line
	}
	return out + tint(" "+strings.ReplaceAll(line, codeBg, ""), "\x1b[48;2;"+lerp(bg0, color, bg)+"m", width+1)
}

func blip(line string, frames, width int) string {
	t := float64(frames) / blipFrames
	return mark(line, yellow, t, 0.35*t, width)
}

func (m model) View() tea.View {
	var b strings.Builder
	if m.help {
		b.WriteString(lipgloss.Place(m.width, m.page(), lipgloss.Center, lipgloss.Center, m.helpMenu()))
		b.WriteByte('\n')
	} else {
		width := m.width - gutter
		for i := m.offset; i < m.offset+m.page(); i++ {
			if i < len(m.lines) {
				line := m.lines[i]
				if m.hover != "" {
					line = underline(line, m.hover)
				}
				line = ansi.Cut(line, m.xoff, m.xoff+width)
				frames, blipping := m.blips[i]
				switch {
				case blipping:
					b.WriteString(blip(line, frames, width))
				case m.matched[i] && i == m.matches[m.cur]:
					b.WriteString(mark(line, orange, 1, 0.3, width))
				case m.matched[i]:
					b.WriteString(mark(line, orange, 0.6, 0, width))
				default:
					b.WriteString("  " + line)
				}
			}
			b.WriteByte('\n')
		}
	}
	b.WriteString(m.status())
	v := tea.NewView(b.String())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeAllMotion
	v.WindowTitle = filepath.Base(m.path)
	return v
}

func (m model) helpMenu() string {
	keyWidth := 0
	for _, b := range bindings {
		keyWidth = max(keyWidth, len(strings.Join(b.keys, " / ")))
	}
	rows := []string{helpTitle.Render("Keyboard shortcuts"), ""}
	for _, b := range bindings {
		rows = append(rows, helpKey.Width(keyWidth+3).Render(strings.Join(b.keys, " / "))+helpDesc.Render(b.desc))
	}
	return helpBox.Render(strings.Join(rows, "\n"))
}

func (m model) status() string {
	if m.err != nil {
		return errStyle.Width(m.width).Render(" " + m.err.Error())
	}
	pct := 100
	if rest := len(m.lines) - m.page(); rest > 0 {
		pct = m.offset * 100 / rest
	}
	s := fmt.Sprintf(" %s  %d%%", filepath.Base(m.path), pct)
	if !m.wrap {
		s += fmt.Sprintf("  nowrap col %d", m.xoff+1)
	}
	if !m.updated.IsZero() {
		s += "  updated " + m.updated.Format("15:04:05")
	}
	switch {
	case m.notice != "":
		s = " " + m.notice
	case m.searching || m.query != "":
		s = " /" + m.query
		if m.searching {
			s += "▏"
		}
		if len(m.matches) > 0 {
			s += fmt.Sprintf("  %d/%d", m.cur+1, len(m.matches))
		} else if m.query != "" {
			s += "  no matches"
		}
		if !m.searching {
			s += "  n/N next/prev  esc clear"
		}
	default:
		s += "  ? help"
	}
	return statusBar.Width(m.width).Render(ansi.Truncate(s, m.width, ""))
}
