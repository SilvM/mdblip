package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/png"
	"math"
	"math/rand/v2"
	"os"
	"os/exec"
	"strings"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/kitty"
	"github.com/charmbracelet/x/term"
	"golang.org/x/sys/unix"
)

const (
	maxCells = 250 // stays inside the placeholder row/column mark table
	// Image pixels per cell width when the terminal does not report its
	// size: an 8px cell at the 2x scale diagrams are rendered at.
	defaultCell = 8 * mermaidScale
	// A cell wider than this is taken to be reported in device pixels on a
	// high-density display, which already match the 2x image.
	maxLogicalCell = 12
)

// imageSupport reports whether inline images can be shown, and if not, why.
func imageSupport() (bool, string) {
	env := os.Getenv
	switch {
	case !term.IsTerminal(os.Stdout.Fd()):
		return false, "not a terminal"
	case env("GHOSTTY_RESOURCES_DIR") == "" && env("KITTY_WINDOW_ID") == "" && !strings.Contains(env("TERM"), "kitty") && !strings.Contains(env("TERM"), "ghostty"):
		return false, "terminal is not Ghostty or Kitty"
	case colorprofile.Detect(os.Stdout, os.Environ()) != colorprofile.TrueColor:
		return false, "terminal is not in true colour"
	}
	if _, err := exec.LookPath("mmdc"); err != nil {
		return false, "mmdc (mermaid CLI) is not installed"
	}
	if env("TMUX") != "" {
		out, err := exec.Command("tmux", "show", "-Av", "allow-passthrough").Output()
		if err == nil && strings.TrimSpace(string(out)) != "on" && strings.TrimSpace(string(out)) != "all" {
			return false, "tmux needs: set -g allow-passthrough on"
		}
	}
	return true, ""
}

// Image IDs belong to the terminal, not the pane, so start somewhere random
// to keep two mdblip panes from overwriting each other's images.
var nextImageID = rand.Uint32N(0xffff00) + 1

func newImageID() uint32 {
	nextImageID = nextImageID%0xfffffe + 1
	return nextImageID
}

func passthrough(seq string) string {
	if os.Getenv("TMUX") != "" {
		return ansi.TmuxPassthrough(seq)
	}
	return seq
}

// transmit sends a PNG to the terminal and creates a virtual placement of
// cols x rows cells, which the placeholder cells then display.
func transmit(id uint32, png []byte, cols, rows int) string {
	data := base64.StdEncoding.EncodeToString(png)
	var b strings.Builder
	opts := []string{"a=T", "U=1", "q=2", "f=100", fmt.Sprintf("i=%d", id), fmt.Sprintf("c=%d", cols), fmt.Sprintf("r=%d", rows)}
	for first := true; first || len(data) > 0; first = false {
		chunk := data[:min(kitty.MaxChunkSize, len(data))]
		data = data[len(chunk):]
		more := "m=0"
		if len(data) > 0 {
			more = "m=1"
		}
		if first {
			b.WriteString(passthrough(ansi.KittyGraphics([]byte(chunk), append(opts, more)...)))
		} else {
			b.WriteString(passthrough(ansi.KittyGraphics([]byte(chunk), more)))
		}
	}
	return b.String()
}

func deleteImage(id uint32) string {
	return passthrough(ansi.KittyGraphics(nil, "a=d", "d=I", "q=2", fmt.Sprintf("i=%d", id)))
}

// placeholderRows returns the text cells that display an image. Every cell
// carries its own row and column mark, so a row still draws correctly when
// it is cut for horizontal scrolling.
func placeholderRows(id uint32, cols, rows int) []string {
	color := fmt.Sprintf("\x1b[38;2;%d;%d;%dm", id>>16&0xff, id>>8&0xff, id&0xff)
	out := make([]string, rows)
	for r := range out {
		var b strings.Builder
		b.WriteString(color)
		for c := range cols {
			b.WriteRune(kitty.Placeholder)
			b.WriteRune(kitty.Diacritic(r))
			b.WriteRune(kitty.Diacritic(c))
		}
		b.WriteString("\x1b[m")
		out[r] = b.String()
	}
	return out
}

// cellSize returns how many image pixels span one cell at the diagram's
// natural size, and the cell's width/height ratio.
func cellSize() (width int, ratio float64) {
	ws, err := unix.IoctlGetWinsize(int(os.Stdout.Fd()), unix.TIOCGWINSZ)
	if err != nil || ws.Xpixel == 0 || ws.Ypixel == 0 || ws.Col == 0 || ws.Row == 0 {
		return defaultCell, 0.5
	}
	w, h := float64(ws.Xpixel)/float64(ws.Col), float64(ws.Ypixel)/float64(ws.Row)
	if w <= maxLogicalCell {
		w, h = w*mermaidScale, h*mermaidScale
	}
	return int(w), w / h
}

// fit returns the cell box for an image of the given pixel size: its natural
// width, shrunk to the available width, with the height following the aspect.
func fit(pxW, pxH, avail, cellW int, ratio float64) (cols, rows int) {
	cols = max(min(pxW/max(cellW, 1), avail, maxCells), 1)
	rows = int(math.Ceil(float64(cols) * float64(pxH) / float64(pxW) * ratio))
	if rows > maxCells {
		cols = max(cols*maxCells/rows, 1)
		rows = maxCells
	}
	return cols, max(rows, 1)
}

func pngSize(png []byte) (w, h int, err error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(png))
	return cfg.Width, cfg.Height, err
}
