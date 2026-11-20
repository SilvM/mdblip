package main

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/kitty"
)

const mermaidDoc = "# Doc\n\nintro\n\n```mermaid\ngraph LR\n  A --> B\n```\n\noutro\n"

func TestMermaidBlocks(t *testing.T) {
	src := "a\n```mermaid\ngraph LR\n```\nb\n```go\nx := 1\n```\n~~~ mermaid\nunterminated\n"
	var texts []string
	kept := mermaidBlocks(src, func(_, text string) bool { texts = append(texts, text); return false })
	if kept != src {
		t.Errorf("unswapped source changed:\n%q", kept)
	}
	if len(texts) != 2 || texts[0] != "graph LR" || texts[1] != "unterminated\n" {
		t.Errorf("blocks found: %q", texts)
	}
	swapped := mermaidBlocks(src, func(string, string) bool { return true })
	if strings.Contains(swapped, "graph LR") || !strings.Contains(swapped, "x := 1") {
		t.Errorf("swap replaced the wrong blocks:\n%q", swapped)
	}
	if n := len(imgMarkRe.FindAllString(swapped, -1)); n != 2 {
		t.Errorf("%d markers, want 2", n)
	}
}

func TestPlaceholderRows(t *testing.T) {
	rows := placeholderRows(0x123456, 12, 3)
	if len(rows) != 3 {
		t.Fatalf("%d rows, want 3", len(rows))
	}
	for r, row := range rows {
		if w := ansi.StringWidth(row); w != 12 {
			t.Errorf("row %d width %d, want 12", r, w)
		}
		if !strings.HasPrefix(row, "\x1b[38;2;18;52;86m") {
			t.Errorf("row %d does not carry the image id as its colour: %q", r, row[:20])
		}
		cut := ansi.Cut(row, 3, 8)
		if w := ansi.StringWidth(cut); w != 5 {
			t.Errorf("cut row %d width %d, want 5", r, w)
		}
		cells := []rune(ansi.Strip(cut))
		if len(cells) != 15 {
			t.Fatalf("cut row %d has %d runes, want 5 cells of 3", r, len(cells))
		}
		for i := 0; i < len(cells); i += 3 {
			if cells[i] != kitty.Placeholder || cells[i+1] != kitty.Diacritic(r) || cells[i+2] != kitty.Diacritic(3+i/3) {
				t.Errorf("cut row %d cell %d lost its row/column marks", r, i/3)
			}
		}
	}
}

func TestTransmit(t *testing.T) {
	t.Setenv("TMUX", "")
	small := transmit(7, []byte("png"), 10, 4)
	if small != "\x1b_Ga=T,U=1,q=2,f=100,i=7,c=10,r=4,m=0;cG5n\x1b\\" {
		t.Errorf("small image: %q", small)
	}
	big := transmit(7, make([]byte, 10000), 10, 4)
	chunks := strings.Split(strings.TrimSuffix(big, "\x1b\\"), "\x1b\\")
	if len(chunks) != 4 {
		t.Fatalf("%d chunks, want 4", len(chunks))
	}
	for i, c := range chunks {
		head, payload, _ := strings.Cut(strings.TrimPrefix(c, "\x1b_G"), ";")
		last := i == len(chunks)-1
		if len(payload) > chunkSize || (!last && len(payload)%4 != 0) {
			t.Errorf("chunk %d payload is %d bytes", i, len(payload))
		}
		if (last && !strings.HasSuffix(head, "m=0")) || (!last && !strings.HasSuffix(head, "m=1")) {
			t.Errorf("chunk %d has wrong continuation flag: %q", i, head)
		}
		if (i == 0) != strings.Contains(head, "i=7") {
			t.Errorf("chunk %d keys: %q", i, head)
		}
	}
	t.Setenv("TMUX", "/tmp/tmux-1/default,1,0")
	if wrapped := transmit(7, []byte("png"), 10, 4); !strings.HasPrefix(wrapped, "\x1bPtmux;\x1b\x1b_G") {
		t.Errorf("not wrapped for tmux: %q", wrapped)
	}
}

func TestFit(t *testing.T) {
	cases := []struct{ pxW, pxH, avail, wantCols, wantRows int }{
		{960, 480, 100, 60, 15},
		{960, 480, 40, 40, 10},
		{160, 16000, 100, 2, 100},
		{16, 16, 100, 1, 1},
	}
	for _, c := range cases {
		cols, rows := fit(c.pxW, c.pxH, c.avail, 16, 0.5)
		if c.pxH == 16000 {
			if rows > maxCells || cols < 1 {
				t.Errorf("tall image: %dx%d", cols, rows)
			}
			continue
		}
		if cols != c.wantCols || rows != c.wantRows {
			t.Errorf("fit(%d,%d,%d) = %dx%d, want %dx%d", c.pxW, c.pxH, c.avail, cols, rows, c.wantCols, c.wantRows)
		}
	}
}

// run executes a command and everything it batches, returning the messages.
func run(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		var out []tea.Msg
		for _, c := range msg {
			out = append(out, run(c)...)
		}
		return out
	case nil:
		return nil
	default:
		return []tea.Msg{msg}
	}
}

func raws(msgs []tea.Msg) string {
	var b strings.Builder
	for _, msg := range msgs {
		if raw, ok := msg.(tea.RawMsg); ok {
			b.WriteString(raw.Msg.(string))
		}
	}
	return b.String()
}

func step(t *testing.T, m model, msg tea.Msg) (model, []tea.Msg) {
	t.Helper()
	next, cmd := m.Update(msg)
	return next.(model), run(cmd)
}

func imageModel(t *testing.T, src string) model {
	t.Helper()
	t.Setenv("TMUX", "")
	path := filepath.Join(t.TempDir(), "doc.md")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	m := model{path: path, src: src, wrap: true, images: true, diagrams: map[string]*diagram{}}
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 30})
	return m
}

func hasImage(m model) bool {
	return strings.ContainsRune(m.View().Content, kitty.Placeholder)
}

func TestDiagramFlow(t *testing.T) {
	defer func(orig func(string) ([]byte, error)) { renderMermaid = orig }(renderMermaid)
	calls := 0
	renderMermaid = func(string) ([]byte, error) { calls++; return testCard("") }

	m := imageModel(t, mermaidDoc)
	started := run(m.startDiagrams())
	if calls != 1 || len(started) != 1 {
		t.Fatalf("%d renders started, want 1", calls)
	}
	if hasImage(m) || !strings.Contains(ansi.Strip(m.View().Content), "A --> B") {
		t.Fatal("pending diagram should show as source")
	}
	if again := run(m.startDiagrams()); len(again) != 0 || calls != 1 {
		t.Fatal("diagram rendered twice")
	}

	m, msgs := step(t, m, started[0])
	if !hasImage(m) || strings.Contains(ansi.Strip(m.View().Content), "A --> B") {
		t.Fatal("finished diagram not shown as image")
	}
	if sent := raws(msgs); strings.Count(sent, "a=T") != 1 {
		t.Fatalf("image transmitted %d times, want 1", strings.Count(sent, "a=T"))
	}
	if len(m.blips) == 0 {
		t.Error("new image did not blip")
	}
	text := ansi.Strip(m.View().Content)
	if !strings.Contains(text, "intro") || !strings.Contains(text, "outro") {
		t.Errorf("text around the image lost:\n%s", text)
	}

	m, msgs = step(t, m, tea.KeyPressMsg{Code: 'j', Text: "j"})
	if raws(msgs) != "" {
		t.Error("scrolling re-sent image data")
	}

	m, msgs = step(t, m, tea.KeyPressMsg{Code: 'm', Text: "m"})
	if hasImage(m) || !strings.Contains(ansi.Strip(m.View().Content), "A --> B") {
		t.Fatal("m did not switch to source")
	}
	m, msgs = step(t, m, tea.KeyPressMsg{Code: 'm', Text: "m"})
	if !hasImage(m) || raws(msgs) != "" || calls != 1 {
		t.Fatal("m back to images should reuse the image already sent")
	}

	m, msgs = step(t, m, tea.WindowSizeMsg{Width: 30, Height: 30})
	if strings.Count(raws(msgs), "a=T") != 1 {
		t.Error("narrower pane did not re-place the image")
	}
	m, msgs = step(t, m, tea.FocusMsg{})
	if strings.Count(raws(msgs), "a=T") != 1 {
		t.Error("focus did not re-send the image")
	}

	os.WriteFile(m.path, []byte("# Doc\n\nno diagram now\n"), 0o644)
	m, msgs = step(t, m, reloadMsg{})
	if hasImage(m) || !strings.Contains(raws(msgs), "a=d") || len(m.diagrams) != 0 {
		t.Error("removed diagram was not deleted")
	}
}

func TestDiagramFailureAndStale(t *testing.T) {
	defer func(orig func(string) ([]byte, error)) { renderMermaid = orig }(renderMermaid)
	renderMermaid = func(string) ([]byte, error) { return nil, errors.New("Parse error on line 2\nmore detail") }

	m := imageModel(t, mermaidDoc)
	started := run(m.startDiagrams())
	m, _ = step(t, m, started[0])
	if hasImage(m) || !strings.Contains(ansi.Strip(m.View().Content), "A --> B") {
		t.Error("failed diagram should stay as source")
	}
	if m.notice != "mermaid: Parse error on line 2" {
		t.Errorf("notice %q", m.notice)
	}

	before := m.View().Content
	m, msgs := step(t, m, diagramMsg{key: "gone", png: []byte("x")})
	if m.View().Content != before || len(msgs) != 0 {
		t.Error("stale result changed the view")
	}
}

func TestImagesOffNeverRenders(t *testing.T) {
	defer func(orig func(string) ([]byte, error)) { renderMermaid = orig }(renderMermaid)
	renderMermaid = func(string) ([]byte, error) { t.Error("renderer ran with images off"); return nil, nil }
	m := imageModel(t, mermaidDoc)
	m.images, m.imagesWhy = false, "terminal is not Ghostty or Kitty"
	run(m.startDiagrams())
	m, _ = step(t, m, tea.KeyPressMsg{Code: 'm', Text: "m"})
	if m.images || !strings.Contains(m.notice, "images unavailable") {
		t.Errorf("toggle on unsupported terminal: images=%v notice=%q", m.images, m.notice)
	}
}

func benchModel(b *testing.B, images bool) model {
	src := strings.Repeat("## Section\n\nSome text with **bold** words and a [link](https://example.com).\n\n```mermaid\ngraph LR\n  A --> B\n```\n\n", 8)
	m := model{path: "x.md", src: src, wrap: true, images: images, diagrams: map[string]*diagram{}}
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 50})
	m = next.(model)
	for _, msg := range run(m.startDiagrams()) {
		next, _ = m.Update(msg)
		m = next.(model)
	}
	if images != strings.ContainsRune(strings.Join(m.lines, ""), kitty.Placeholder) {
		b.Fatal("bench document not in the expected mode")
	}
	return m
}

func BenchmarkFrame(b *testing.B) {
	for _, mode := range []struct {
		name   string
		images bool
	}{{"source", false}, {"images", true}} {
		m := benchModel(b, mode.images)
		b.Run(mode.name, func(b *testing.B) {
			for b.Loop() {
				m.View()
			}
		})
	}
}

func BenchmarkReload(b *testing.B) {
	for _, mode := range []struct {
		name   string
		images bool
	}{{"source", false}, {"images", true}} {
		m := benchModel(b, mode.images)
		b.Run(mode.name, func(b *testing.B) {
			for b.Loop() {
				m.refresh(true, true)
			}
		})
	}
}

// testCard stands in for a real diagram: a framed gradient that makes
// scaling, cropping and colour problems easy to see.
func testCard(string) ([]byte, error) {
	const w, h = 960, 480
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			c := color.RGBA{uint8(x * 255 / w), uint8(y * 255 / h), 0x66, 0xff}
			switch {
			case x < 8 || y < 8 || x >= w-8 || y >= h-8:
				c = color.RGBA{0xfa, 0xbd, 0x2f, 0xff}
			case (x/60+y/60)%2 == 0 && x > w/3 && x < 2*w/3 && y > h/3 && y < 2*h/3:
				c = color.RGBA{0x28, 0x28, 0x28, 0xff}
			}
			img.SetRGBA(x, y, c)
		}
	}
	var b bytes.Buffer
	err := png.Encode(&b, img)
	return b.Bytes(), err
}

func TestMmdcUsesCache(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv("PATH", "")
	src := "graph LR\n  A --> B"
	if _, err := mmdc(src); err == nil {
		t.Fatal("render without mmdc on PATH should fail")
	}
	if !strings.HasPrefix(cachePath(src), os.TempDir()) {
		t.Fatalf("cache %q is outside the temp dir", cachePath(src))
	}
	if cachePath(src) == cachePath(src+" ") {
		t.Fatal("different diagrams share a cache file")
	}
	want, _ := testCard("")
	if err := os.WriteFile(cachePath(src), want, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := mmdc(src)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("cached diagram not reused: %v", err)
	}
	leftovers, _ := filepath.Glob(filepath.Join(cacheDir(), "*.tmp.png"))
	if len(leftovers) != 0 {
		t.Errorf("temp files left behind: %v", leftovers)
	}
}
