package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// Private-use sentinel wrapping a diagram key in the text handed to Glamour.
const imgMark = ""

var (
	mermaidOpenRe = regexp.MustCompile("^ {0,3}(```+|~~~+)[ \t]*mermaid[ \t]*$")
	imgMarkRe     = regexp.MustCompile(imgMark + "([0-9a-f]+)" + imgMark)
)

type diagram struct {
	id         uint32
	png        []byte
	pxW, pxH   int
	cols, rows int
	lines      []string
	failed     bool
}

func (d *diagram) ready() bool { return d != nil && d.png != nil }

type diagramMsg struct {
	key string
	png []byte
	err error
}

// renderMermaid turns mermaid source into a PNG.
var renderMermaid = mmdc

const (
	mermaidScale  = 2
	mmdcTimeout   = 60 * time.Second
	mermaidConfig = `{"theme":"base","themeVariables":{"darkMode":true,"fontFamily":"monospace","background":"#282828","primaryColor":"#3c3836","primaryTextColor":"#ebdbb2","primaryBorderColor":"#d79921","secondaryColor":"#504945","secondaryTextColor":"#ebdbb2","secondaryBorderColor":"#689d6a","tertiaryColor":"#32302f","tertiaryTextColor":"#ebdbb2","tertiaryBorderColor":"#665c54","lineColor":"#a89984","textColor":"#ebdbb2","titleColor":"#fabd2f","edgeLabelBackground":"#282828","clusterBkg":"#32302f","clusterBorder":"#665c54","noteBkgColor":"#504945","noteTextColor":"#ebdbb2","noteBorderColor":"#d79921","actorBkg":"#3c3836","actorBorder":"#d79921","actorTextColor":"#ebdbb2","actorLineColor":"#a89984","signalColor":"#a89984","signalTextColor":"#ebdbb2","labelBoxBkgColor":"#3c3836","labelBoxBorderColor":"#d79921","labelTextColor":"#ebdbb2","loopTextColor":"#ebdbb2","activationBkgColor":"#504945","activationBorderColor":"#d79921"}}`
)

// Each mmdc run starts a headless browser, so run at most two at a time.
var mmdcSlots = make(chan struct{}, 2)

// cacheDir lives in the system temp directory, which the OS clears itself.
func cacheDir() string { return filepath.Join(os.TempDir(), "mdblip") }

func cachePath(src string) string {
	key := sha256.Sum256([]byte(mermaidConfig + "\x00" + strconv.Itoa(mermaidScale) + "\x00" + src))
	return filepath.Join(cacheDir(), fmt.Sprintf("%x.png", key[:16]))
}

// mmdc renders a diagram with the mermaid CLI, reusing a cached PNG when the
// same diagram has been rendered before.
func mmdc(src string) ([]byte, error) {
	path := cachePath(src)
	if png, err := os.ReadFile(path); err == nil {
		return png, nil
	}
	if err := os.MkdirAll(cacheDir(), 0o755); err != nil {
		return nil, err
	}
	config := filepath.Join(cacheDir(), "mermaid-config.json")
	if err := os.WriteFile(config, []byte(mermaidConfig), 0o644); err != nil {
		return nil, err
	}
	mmdcSlots <- struct{}{}
	defer func() { <-mmdcSlots }()
	ctx, cancel := context.WithTimeout(context.Background(), mmdcTimeout)
	defer cancel()
	tmp := strings.TrimSuffix(path, ".png") + fmt.Sprintf(".%d.tmp.png", os.Getpid())
	defer os.Remove(tmp)
	cmd := exec.CommandContext(ctx, "mmdc", "-i", "-", "-o", tmp, "-b", "transparent", "-s", strconv.Itoa(mermaidScale), "-c", config, "-q")
	cmd.Stdin = strings.NewReader(src)
	if out, err := cmd.CombinedOutput(); err != nil {
		for _, line := range strings.Split(string(out), "\n") {
			if line = strings.TrimSpace(line); line != "" {
				return nil, errors.New(line)
			}
		}
		return nil, err
	}
	png, err := os.ReadFile(tmp)
	if err != nil {
		return nil, err
	}
	// Rename so a concurrent reader never sees a half-written file.
	return png, os.Rename(tmp, path)
}

func diagramKey(src string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(src)))[:16]
}

// mermaidBlocks walks the source and calls swap for each mermaid fence with
// its key and text. If swap returns true the fence is replaced by a marker
// line; otherwise it is left as it is.
func mermaidBlocks(src string, swap func(key, text string) bool) string {
	lines := strings.Split(src, "\n")
	out := make([]string, 0, len(lines))
	for i := 0; i < len(lines); i++ {
		open := mermaidOpenRe.FindStringSubmatch(lines[i])
		if open == nil {
			out = append(out, lines[i])
			continue
		}
		end := i + 1
		for end < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[end]), open[1]) {
			end++
		}
		text := strings.Join(lines[i+1:end], "\n")
		key := diagramKey(text)
		if swap(key, text) {
			out = append(out, "", imgMark+key+imgMark, "")
		} else {
			out = append(out, lines[i:min(end+1, len(lines))]...)
		}
		i = end
	}
	return strings.Join(out, "\n")
}

// startDiagrams begins rendering every mermaid block that has not been
// rendered yet, and forgets diagrams that have left the document.
func (m *model) startDiagrams() tea.Cmd {
	if !m.images {
		return nil
	}
	var cmds []tea.Cmd
	seen := map[string]bool{}
	mermaidBlocks(m.src, func(key, text string) bool {
		seen[key] = true
		if m.diagrams[key] == nil {
			m.diagrams[key] = &diagram{}
			cmds = append(cmds, func() tea.Msg {
				png, err := renderMermaid(text)
				return diagramMsg{key, png, err}
			})
		}
		return false
	})
	for key, d := range m.diagrams {
		if !seen[key] {
			if d.ready() {
				cmds = append(cmds, tea.Raw(deleteImage(d.id)))
			}
			delete(m.diagrams, key)
		}
	}
	return tea.Batch(cmds...)
}

// diagramDone stores a finished render; a result for a diagram that has
// since left the document is dropped.
func (m *model) diagramDone(msg diagramMsg) tea.Cmd {
	d := m.diagrams[msg.key]
	if d == nil {
		return nil
	}
	if msg.err == nil {
		d.pxW, d.pxH, msg.err = pngSize(msg.png)
	}
	if msg.err != nil {
		d.failed = true
		m.notice = "mermaid: " + strings.SplitN(msg.err.Error(), "\n", 2)[0]
		return nil
	}
	d.id, d.png = newImageID(), msg.png
	return m.refresh(false)
}

// rendering counts the diagrams still being turned into images.
func (m *model) rendering() int {
	n := 0
	for _, d := range m.diagrams {
		if m.images && !d.ready() && !d.failed {
			n++
		}
	}
	return n
}

// renderDoc renders the document, showing finished diagrams as images. The
// returned command sends any image whose placement is new or has changed.
func (m *model) renderDoc() ([]string, []heading, tea.Cmd) {
	width := m.width - gutter
	if !m.images {
		lines, heads := takeHeadings(renderMarked(m.src, width, m.wrap))
		return lines, heads, nil
	}
	src := mermaidBlocks(m.src, func(key, _ string) bool { return m.diagrams[key].ready() })
	lines := renderMarked(src, width, m.wrap)
	out := make([]string, 0, len(lines))
	var cmds []tea.Cmd
	cellW, ratio := cellSize()
	for _, l := range lines {
		var match []string
		if strings.Contains(l, imgMark) {
			match = imgMarkRe.FindStringSubmatch(ansi.Strip(l))
		}
		if match == nil {
			out = append(out, l)
			continue
		}
		d := m.diagrams[match[1]]
		if cols, rows := fit(d.pxW, d.pxH, width, cellW, ratio); cols != d.cols || rows != d.rows {
			// A resized image gets a new ID: terminals may keep showing the
			// old placement for an ID they have already drawn.
			if d.cols > 0 {
				cmds = append(cmds, tea.Raw(deleteImage(d.id)))
				d.id = newImageID()
			}
			d.cols, d.rows = cols, rows
			d.lines = placeholderRows(d.id, cols, rows)
			cmds = append(cmds, tea.Raw(transmit(d.id, d.png, cols, rows)))
		}
		out = append(out, d.lines...)
	}
	out, heads := takeHeadings(out)
	return out, heads, tea.Batch(cmds...)
}

// resend transmits every finished image again, for a terminal that may have
// dropped them while the pane was hidden.
func (m *model) resend() tea.Cmd {
	if !m.images {
		return nil
	}
	var cmds []tea.Cmd
	for _, d := range m.diagrams {
		if d.cols > 0 {
			cmds = append(cmds, tea.Raw(transmit(d.id, d.png, d.cols, d.rows)))
		}
	}
	return tea.Batch(cmds...)
}
