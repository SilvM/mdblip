package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
)

func main() {
	headless := flag.Bool("headless", false, "print one frame to stdout and exit, without a terminal")
	size := flag.String("size", "80x24", "headless frame size as WIDTHxHEIGHT")
	keys := flag.String("keys", "", "headless: space-separated keys to press before printing, e.g. 'w l ?'")
	color := flag.Bool("ansi", false, "headless: keep colour escape codes in the output")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: mdv [flags] <file.md>")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}
	path := flag.Arg(0)
	data, err := os.ReadFile(path)
	if err != nil {
		fail(err)
	}
	m := model{path: path, src: string(data), wrap: true, diagrams: map[string]*diagram{}}
	switch {
	case *headless:
		frame, err := frame(m, *size, *keys)
		if err != nil {
			fail(err)
		}
		if !*color {
			frame = ansi.Strip(frame)
		}
		fmt.Println(frame)
	case !term.IsTerminal(os.Stdout.Fd()):
		lipgloss.Println(strings.Join(render(m.src, 80, true), "\n"))
	default:
		m.images, m.imagesWhy = imageSupport()
		if strings.HasPrefix(m.imagesWhy, "tmux") {
			m.notice = "mermaid images off: " + m.imagesWhy
		}
		p := tea.NewProgram(m)
		stop, err := watch(path, func() { p.Send(reloadMsg{}) })
		if err != nil {
			fail(err)
		}
		defer stop()
		final, err := p.Run()
		if err != nil {
			fail(err)
		}
		for _, d := range final.(model).diagrams {
			if d.ready() {
				fmt.Print(deleteImage(d.id))
			}
		}
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

// frame runs the model without a terminal: it sizes the window, presses the
// given keys and returns what the screen would show.
func frame(m model, size, keys string) (string, error) {
	var width, height int
	if _, err := fmt.Sscanf(size, "%dx%d", &width, &height); err != nil || width < 1 || height < 1 {
		return "", fmt.Errorf("invalid -size %q, want WIDTHxHEIGHT", size)
	}
	next, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	m = next.(model)
	for _, key := range strings.Fields(keys) {
		m, _ = m.press(key)
	}
	return m.View().Content, nil
}
