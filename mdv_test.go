package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

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
