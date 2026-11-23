package main

import (
	"regexp"
	"strings"

	"charm.land/glamour/v2"
	"charm.land/glamour/v2/ansi"
	xansi "github.com/charmbracelet/x/ansi"
)

const (
	bg0    = "#282828"
	bg1    = "#3c3836"
	bg3    = "#665c54"
	fg     = "#ebdbb2"
	gray   = "#a89984"
	red    = "#fb4934"
	green  = "#b8bb26"
	yellow = "#fabd2f"
	blue   = "#83a598"
	purple = "#d3869b"
	aqua   = "#8ec07c"
	orange = "#fe8019"
)

// Private-use sentinels marking code block boundaries and the rule under a
// table's header in Glamour's output.
const (
	codeStart = "\ue000"
	codeEnd   = "\ue001"
	tableRule = "\ue002"
	codeBg    = "\x1b[48;2;60;56;54m"
	rowBg     = "\x1b[48;2;50;48;47m"
	dim       = "\x1b[38;2;146;131;116m"
)

var resetRe = regexp.MustCompile(`\x1b\[0?m`)

// tint paints a full-width background, re-applying it after every reset so
// it survives the line's own styling.
func tint(line, bg string, width int) string {
	pad := strings.Repeat(" ", max(width-xansi.StringWidth(line), 0))
	return bg + resetRe.ReplaceAllString(line, "\x1b[m"+bg) + pad + "\x1b[m"
}

func ptr[T any](v T) *T { return &v }

func headingStyle(level int, color string) ansi.StyleBlock {
	prefix := string(headBase+rune(level)) + strings.Repeat("#", level) + " "
	return ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Prefix: prefix, Color: ptr(color)}}
}

var gruvbox = ansi.StyleConfig{
	Document: ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Color: ptr(fg)}},
	BlockQuote: ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{Color: ptr(gray), Italic: ptr(true)},
		Indent:         ptr(uint(1)),
		IndentToken:    ptr("│ "),
	},
	List: ansi.StyleList{LevelIndent: 2},
	Heading: ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{BlockSuffix: "\n", Bold: ptr(true)},
	},
	H1:            headingStyle(1, yellow),
	H2:            headingStyle(2, orange),
	H3:            headingStyle(3, aqua),
	H4:            headingStyle(4, blue),
	H5:            headingStyle(5, purple),
	H6:            headingStyle(6, gray),
	Strikethrough: ansi.StylePrimitive{CrossedOut: ptr(true)},
	Emph:          ansi.StylePrimitive{Italic: ptr(true), Color: ptr(purple)},
	Strong:        ansi.StylePrimitive{Bold: ptr(true), Color: ptr(orange)},
	HorizontalRule: ansi.StylePrimitive{
		Color:  ptr(bg3),
		Format: "\n────────\n",
	},
	Item:        ansi.StylePrimitive{BlockPrefix: "• "},
	Enumeration: ansi.StylePrimitive{BlockPrefix: ". ", Color: ptr(aqua)},
	Task:        ansi.StyleTask{Ticked: "[✓] ", Unticked: "[ ] "},
	Link:        ansi.StylePrimitive{Color: ptr(blue)},
	LinkText:    ansi.StylePrimitive{Color: ptr(aqua), Bold: ptr(true)},
	Image:       ansi.StylePrimitive{Color: ptr(blue), Underline: ptr(true)},
	ImageText:   ansi.StylePrimitive{Color: ptr(aqua), Format: "Image: {{.text}} →"},
	Code: ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{Color: ptr(green), BackgroundColor: ptr(bg1), Prefix: " ", Suffix: " "},
	},
	CodeBlock: ansi.StyleCodeBlock{
		StyleBlock: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{Color: ptr(fg), BlockPrefix: codeStart + "\n", BlockSuffix: codeEnd + "\n"},
			Margin:         ptr(uint(2)),
		},
		Theme: "gruvbox",
	},
	Table: ansi.StyleTable{
		StyleBlock:      ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Color: ptr(fg)}},
		ColumnSeparator: ptr("│"),
		CenterSeparator: ptr("┼"),
		RowSeparator:    ptr(tableRule),
	},
	DefinitionDescription: ansi.StylePrimitive{BlockPrefix: "\n→ "},
}

var (
	renderer      *glamour.TermRenderer
	rendererWidth int
	striped       = true
)

// render returns the rendered lines; with wrap off, lines keep their natural
// length and the caller scrolls horizontally.
func render(src string, width int, wrap bool) []string {
	lines, _ := takeHeadings(renderMarked(src, width, wrap))
	return lines
}

// renderMarked is render with the heading sentinels still in place.
func renderMarked(src string, width int, wrap bool) []string {
	width = max(width, 10)
	wrapWidth := 0
	if wrap {
		wrapWidth = width
	}
	if renderer == nil || rendererWidth != wrapWidth {
		r, err := glamour.NewTermRenderer(glamour.WithStyles(gruvbox), glamour.WithWordWrap(wrapWidth), glamour.WithChromaFormatter("terminal16m"))
		if err != nil {
			return []string{err.Error()}
		}
		renderer, rendererWidth = r, wrapWidth
	}
	out, err := renderer.Render(src)
	if err != nil {
		return []string{err.Error()}
	}
	lines := strings.Split(out, "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	for _, l := range lines {
		width = max(width, xansi.StringWidth(l))
	}
	inCode := false
	for i, l := range lines {
		switch {
		case strings.Contains(l, codeStart):
			inCode = true
			lines[i] = tint("", codeBg, width)
		case strings.Contains(l, codeEnd):
			inCode = false
			lines[i] = tint("", codeBg, width)
		case inCode:
			lines[i] = tint(l, codeBg, width)
		}
	}
	stripe(lines)
	return lines
}

// stripe tints every other body row of each table. A row whose cells wrap
// takes several lines, which cannot be told apart from separate rows.
func stripe(lines []string) {
	for i := 0; i < len(lines); i++ {
		if !strings.Contains(lines[i], tableRule) {
			continue
		}
		lines[i] = strings.ReplaceAll(lines[i], tableRule, "─")
		width := xansi.StringWidth(lines[i])
		for row := 0; i+1 < len(lines) && strings.TrimSpace(xansi.Strip(lines[i+1])) != ""; row++ {
			i++
			if striped && row%2 == 1 {
				lines[i] = tint(lines[i], rowBg, width)
			}
		}
	}
}
