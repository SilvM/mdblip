# mdblip
A terminal markdown viewer that follows the file: save in your editor and the change appears at once, with the changed lines flashing in. Gruvbox theme, striped tables, syntax highlighting, fuzzy search, folding by heading, clickable links and mermaid diagrams.

## Usage
```sh
mdblip notes.md
```
Press `?` inside `mdblip` for the keyboard shortcuts.

### Keys
| Key | Does |
|---|---|
| `j` / `k` | line down / up |
| `d` / `u` | half page down / up |
| `space`, `f` / `b` | page down / up |
| `g` / `G` | top / bottom |
| `}` / `{` | next / previous paragraph |
| `tab` | fold or open the section in view |
| `z` | fold or open all sections |
| `/` | fuzzy search; `enter` keeps it, `esc` clears it |
| `n` / `p` | next / previous match |
| `w` | toggle line wrap |
| `l` / `h`, `0` | scroll right / left when unwrapped, back to first column |
| `t` | toggle striped table rows |
| `m` | toggle mermaid diagrams as images |
| `?` | help |
| `q` | quit |

### Mouse
- Click a link to open it; links underline on hover.
- Click a heading to fold or open its section.
- The wheel scrolls.

## Dependencies
### Build
- Go 1.27.3 or newer
- `make`

### Runtime
None for the viewer itself. Two features use tools from your system:

| Feature | Needs |
|---|---|
| Click to open links | `open` (macOS) or `xdg-open` (Linux) |
| Mermaid diagrams as images | [`mmdc`](https://github.com/mermaid-js/mermaid-cli) on `PATH`, a Ghostty or Kitty terminal in true colour, and under tmux `set -g allow-passthrough on` |

Without the mermaid requirements, diagrams are shown as highlighted source.

### Go libraries
Fetched automatically by the build; versions are pinned in `go.mod`.

| Library | Used for |
|---|---|
| `charm.land/bubbletea/v2` | terminal UI loop, keys and mouse |
| `charm.land/glamour/v2` | markdown rendering, including tables |
| `charm.land/lipgloss/v2` | styling the status bar and help menu |
| `github.com/alecthomas/chroma/v2` | syntax highlighting in code blocks |
| `github.com/charmbracelet/x/ansi` | escape-sequence handling and the image protocol |
| `github.com/charmbracelet/x/term` | terminal detection |
| `github.com/charmbracelet/colorprofile` | true-colour detection |
| `github.com/fsnotify/fsnotify` | watching the file for saves |
| `golang.org/x/sys` | reading the terminal's cell size |

## Development
```sh
make            # go mod tidy, vet + tests, build ./mdblip
make install    # all of the above, then install to ~/.local/bin
```

| Target | Does |
|---|---|
| `make tidy` | `go mod tidy` |
| `make test` | `go vet` and `go test` |
| `make build` | builds `./mdblip` |
| `make install` | installs to `$(PREFIX)/bin`, default `~/.local/bin`; override with `make install PREFIX=/some/dir` |
| `make clean` | removes `./mdblip` |

### Testing without a terminal
`-headless` prints one frame and exits, which is handy for checking a change or scripting a test:
```sh
./mdblip -headless -size 80x24 -keys 'j w ?' notes.md
```
`-keys` takes the same key names as the `?` menu; add `-ansi` to keep colour codes.

### Checking dependencies for vulnerabilities
```sh
go install golang.org/x/vuln/cmd/govulncheck@latest
govulncheck ./...
```
