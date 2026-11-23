package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestChanged(t *testing.T) {
	cases := []struct {
		name     string
		old, cur []string
		want     []int
	}{
		{"noop", []string{"a", "b"}, []string{"a", "b"}, nil},
		{"insert", []string{"a", "c"}, []string{"a", "b", "c"}, []int{1}},
		{"edit", []string{"a", "b", "c"}, []string{"a", "x", "c"}, []int{1}},
		{"delete", []string{"a", "b", "c"}, []string{"a", "c"}, nil},
		{"append", []string{"a"}, []string{"a", "b", "c"}, []int{1, 2}},
		{"mixed", []string{"a", "b", "c", "d"}, []string{"a", "x", "c", "y", "d"}, []int{1, 3}},
	}
	for _, c := range cases {
		want := map[int]bool{}
		for _, i := range c.want {
			want[i] = true
		}
		if got := changed(c.old, c.cur); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: got %v, want %v", c.name, got, want)
		}
	}
}

func TestRenderTable(t *testing.T) {
	lines := render("| name | qty |\n|---|---|\n| apple | 1 |\n| fig | 22 |\n", 40, true)
	plain := make([]string, len(lines))
	for i, l := range lines {
		plain[i] = ansi.Strip(l)
	}
	out := strings.Join(plain, "\n")
	for _, want := range []string{"name", "apple", "fig", "│", "─"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	col := -1
	for _, l := range plain {
		if i := strings.Index(l, "│"); i >= 0 {
			if col >= 0 && i != col {
				t.Fatalf("columns not aligned:\n%s", out)
			}
			col = i
		}
	}
}

func TestBlipKeepsWidthAndText(t *testing.T) {
	line := "\x1b[1mbold\x1b[m plain \x1b[0m"
	got := blip(line, blipFrames, 20)
	if w := ansi.StringWidth(got); w != 20+gutter {
		t.Errorf("width %d, want %d", w, 20+gutter)
	}
	if !strings.Contains(ansi.Strip(got), "bold plain") {
		t.Errorf("text lost: %q", ansi.Strip(got))
	}
}

func TestWatchRenameSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "note.md")
	if err := os.WriteFile(path, []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	var hits atomic.Int32
	stop, err := watch(path, func() { hits.Add(1) })
	if err != nil {
		t.Fatal(err)
	}
	defer stop()

	// nvim-style save: twice, to prove the watch survives the rename.
	for _, body := range []string{"two", "three"} {
		before := hits.Load()
		tmp := filepath.Join(dir, "note.md.tmp")
		if err := os.WriteFile(tmp, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(tmp, path); err != nil {
			t.Fatal(err)
		}
		time.Sleep(10 * debounce)
		if got := hits.Load() - before; got != 1 {
			t.Fatalf("save %q fired %d reloads, want 1", body, got)
		}
	}
}

func TestShellHighlight(t *testing.T) {
	plain := render("```\ncp -r a b\n```\n", 40, true)
	shell := render("```bash\ncp -r a b\n```\n", 40, true)
	if strings.Join(plain, "\n") == strings.Join(shell, "\n") {
		t.Fatal("bash block rendered without highlighting")
	}
}

func TestUnwrap(t *testing.T) {
	src := strings.Repeat("word ", 40) + "\n"
	if n := len(render(src, 40, true)); n < 2 {
		t.Fatalf("wrapped render has %d lines, want several", n)
	}
	lines := render(src, 40, false)
	if len(lines) != 1 || ansi.StringWidth(lines[0]) < 199 {
		t.Fatalf("unwrapped render: %d lines, width %d", len(lines), ansi.StringWidth(lines[0]))
	}
}

func TestEveryBindingHasHelp(t *testing.T) {
	seen := map[string]bool{}
	for _, b := range bindings {
		if b.desc == "" || len(b.keys) == 0 {
			t.Errorf("binding %v lacks keys or description", b.keys)
		}
		for _, k := range b.keys {
			if seen[k] {
				t.Errorf("key %q bound twice", k)
			}
			seen[k] = true
		}
	}
}

func TestHeadlessFrame(t *testing.T) {
	m := model{path: "x.md", src: "# Title\n" + strings.Repeat("word ", 30) + "\n", wrap: true}
	wrapped, err := frame(m, "40x10", "")
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Split(wrapped, "\n"); len(lines) != 10 {
		t.Errorf("frame has %d lines, want 10", len(lines))
	}
	unwrapped, _ := frame(m, "40x10", "w l")
	if !strings.Contains(ansi.Strip(unwrapped), "nowrap col 9") {
		t.Errorf("keys not applied:\n%s", ansi.Strip(unwrapped))
	}
	help, _ := frame(m, "60x24", "?")
	if !strings.Contains(ansi.Strip(help), "Keyboard shortcuts") {
		t.Error("help menu not shown")
	}
	if _, err := frame(m, "bogus", ""); err == nil {
		t.Error("bad size accepted")
	}
}

func TestFuzzy(t *testing.T) {
	cases := []struct {
		query, line string
		want        bool
	}{
		{"table", "## Table", true},
		{"tbl", "## Table", true},
		{"conn", "lz-tools connectivity", true},
		{"cnty", "lz-tools connectivity", false},
		{"ab", "a long gap then b", false},
		{"", "anything", false},
	}
	for _, c := range cases {
		if got := fuzzy(c.query, c.line); got != c.want {
			t.Errorf("fuzzy(%q, %q) = %v, want %v", c.query, c.line, got, c.want)
		}
	}
}

func TestSearchNavigation(t *testing.T) {
	src := "# Top\n\napple one\n\n" + strings.Repeat("filler\n\n", 30) + "apple two\n"
	m := model{path: "x.md", src: src, wrap: true}
	typed, _ := frame(m, "40x10", "/ a p l")
	if !strings.Contains(ansi.Strip(typed), "/apl▏  1/2") {
		t.Errorf("prompt not shown:\n%s", ansi.Strip(typed))
	}
	next, _ := frame(m, "40x10", "/ a p l enter n")
	if s := ansi.Strip(next); !strings.Contains(s, "apple two") || !strings.Contains(s, "2/2") {
		t.Errorf("n did not jump to second match:\n%s", s)
	}
	cleared, _ := frame(m, "40x10", "/ a p l enter esc")
	if !strings.Contains(ansi.Strip(cleared), "? help") {
		t.Errorf("esc did not clear search:\n%s", ansi.Strip(cleared))
	}
}

func TestParagraphJump(t *testing.T) {
	src := "one\n\ntwo\ntwo b\n\n```\ncode\n```\n\nthree\n\n" + strings.Repeat("tail\n\n", 20)
	m := model{path: "x.md", src: src, wrap: true}
	top := func(keys string) string {
		out, _ := frame(m, "40x6", keys)
		lines := strings.Split(ansi.Strip(out), "\n")
		return strings.TrimSpace(lines[0]) + "|" + strings.TrimSpace(lines[1])
	}
	for keys, want := range map[string]string{
		"}":     "|two two b",
		"} }":   "|",
		"} } }": "|three",
		"} } {": "|two two b",
		"} { {": "one|",
	} {
		if got := top(keys); got != want {
			t.Errorf("keys %q: top is %q, want %q", keys, got, want)
		}
	}
}

func TestClickOpensLink(t *testing.T) {
	var opened []string
	defer func(orig func(string) error) { openURL = orig }(openURL)
	openURL = func(target string) error { opened = append(opened, target); return nil }

	var m tea.Model = model{path: "x.md", src: "see [the docs](https://example.com/docs) and [local](other.md)\n", wrap: true}
	m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 10})
	plain := ansi.Strip(m.(model).lines[0])
	click := func(word string) model {
		x := ansi.StringWidth(plain[:strings.Index(plain, word)]) + gutter + 1
		next, _ := m.Update(tea.MouseClickMsg{X: x, Y: 0, Button: tea.MouseLeft})
		return next.(model)
	}
	click("see")
	if len(opened) != 0 {
		t.Fatalf("plain text opened %v", opened)
	}
	click("the docs")
	click("https://example.com")
	if len(opened) != 2 || opened[0] != "https://example.com/docs" || opened[1] != opened[0] {
		t.Fatalf("opened %v, want the docs link twice", opened)
	}
	if got := click("local"); len(opened) != 2 || !strings.Contains(got.notice, "not a web link") {
		t.Fatalf("relative link: opened %v, notice %q", opened, got.notice)
	}
}

func TestWrappedLinkStaysClickable(t *testing.T) {
	lines := render("[a fairly long link text that wraps](https://example.com/a/b/c/d/e)\n", 20, true)
	for i, l := range lines {
		if strings.TrimSpace(ansi.Strip(l)) != "" && linkAt(l, 1) == "" {
			t.Errorf("line %d of wrapped link is not clickable: %q", i, ansi.Strip(l))
		}
	}
}

func TestHoverUnderlinesLink(t *testing.T) {
	var m tea.Model = model{path: "x.md", src: "see [the docs](https://example.com/docs) and [other](https://example.org)\n", wrap: true}
	m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 10})
	if strings.Contains(m.View().Content, "\x1b[4m") {
		t.Fatal("link underlined without hover")
	}
	plain := ansi.Strip(m.(model).lines[0])
	hover := func(word string) string {
		x := ansi.StringWidth(plain[:strings.Index(plain, word)]) + gutter + 1
		next, _ := m.Update(tea.MouseMotionMsg{X: x, Y: 0})
		return next.View().Content
	}
	over := hover("the docs")
	if n := strings.Count(over, "\x1b[4m"); n == 0 {
		t.Fatal("hovered link not underlined")
	}
	if ansi.Strip(over) != ansi.Strip(m.View().Content) {
		t.Error("hover changed the text")
	}
	if i := strings.Index(over, "other"); strings.LastIndex(over[:i], "\x1b[4m") > strings.LastIndex(over[:i], "\x1b[24m") {
		t.Error("underline leaked onto the other link")
	}
	if strings.Contains(hover("see"), "\x1b[4m") {
		t.Error("underline stayed after moving off the link")
	}
}

const foldDoc = "# Top\n\nintro\n\n## One\n\nalpha\n\n### Deep\n\nbeta\n\n## Two\n\ngamma\n"

func shown(t *testing.T, m model, size, keys string) []string {
	t.Helper()
	out, err := frame(m, size, keys)
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, l := range strings.Split(ansi.Strip(out), "\n") {
		if l = strings.TrimSpace(l); l != "" && l != "~" {
			lines = append(lines, l)
		}
	}
	return lines[:len(lines)-1]
}

func TestFoldKeys(t *testing.T) {
	m := model{path: "x.md", src: foldDoc, wrap: true}
	cases := []struct {
		size, keys string
		want       []string
	}{
		{"40x20", "", []string{"# Top", "intro", "## One", "alpha", "### Deep", "beta", "## Two", "gamma"}},
		{"40x20", "tab", []string{"▸ # Top ⋯ 14 lines"}},
		{"40x20", "tab tab", []string{"# Top", "intro", "## One", "alpha", "### Deep", "beta", "## Two", "gamma"}},
		{"40x6", "} } tab", []string{"▸ ## One ⋯ 7 lines", "## Two", "gamma"}},
		{"40x20", "z", []string{"▸ # Top ⋯ 14 lines"}},
		{"40x20", "z tab", []string{"# Top", "intro", "▸ ## One ⋯ 7 lines", "▸ ## Two ⋯ 2 lines"}},
		{"40x20", "z z", []string{"# Top", "intro", "## One", "alpha", "### Deep", "beta", "## Two", "gamma"}},
	}
	for _, c := range cases {
		if got := shown(t, m, c.size, c.keys); !reflect.DeepEqual(got, c.want) {
			t.Errorf("keys %q:\ngot  %q\nwant %q", c.keys, got, c.want)
		}
	}
}

func TestFoldClickAndReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "doc.md")
	os.WriteFile(path, []byte(foldDoc), 0o644)
	var m tea.Model = model{path: path, src: foldDoc, wrap: true}
	m, _ = m.Update(tea.WindowSizeMsg{Width: 40, Height: 20})
	row := func(text string) int {
		for i, l := range m.(model).lines {
			if strings.Contains(ansi.Strip(l), text) {
				return i
			}
		}
		return -1
	}
	m, _ = m.Update(tea.MouseClickMsg{X: 5, Y: row("## One"), Button: tea.MouseLeft})
	if row("alpha") >= 0 || row("## Two") < 0 {
		t.Fatal("click on a heading did not fold its section")
	}
	m, _ = m.Update(tea.MouseClickMsg{X: 5, Y: row("intro"), Button: tea.MouseLeft})
	if row("alpha") >= 0 {
		t.Fatal("click on plain text changed the folds")
	}

	os.WriteFile(path, []byte(strings.Replace(foldDoc, "gamma", "gamma edited", 1)), 0o644)
	m, _ = m.Update(reloadMsg{})
	if row("alpha") >= 0 || row("gamma edited") < 0 {
		t.Fatal("edit outside the fold should leave it folded")
	}
	if len(m.(model).blips) == 0 {
		t.Error("edited line did not blip")
	}

	os.WriteFile(path, []byte(strings.Replace(foldDoc, "beta", "beta edited", 1)), 0o644)
	m, _ = m.Update(reloadMsg{})
	if row("beta edited") < 0 {
		t.Fatal("edit inside a folded section did not open it")
	}
	if got := m.(model).blips[row("beta edited")]; got == 0 {
		t.Error("line edited inside the fold did not blip")
	}
}
