package main

import (
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
)

// shellLexer wraps chroma's bash lexer, which leaves external commands and
// flags as plain text, and marks them as commands and flags.
type shellLexer struct{ chroma.Lexer }

var (
	cmdPrefixes = map[string]bool{"sudo": true, "xargs": true, "exec": true, "time": true, "env": true, "watch": true}
	cmdStarters = map[string]bool{"|": true, "||": true, "&&": true, ";": true, "&": true, "(": true, "$(": true, "`": true, "then": true, "do": true, "else": true, "elif": true, "if": true, "while": true, "!": true}
)

func init() {
	lexers.Register(shellLexer{lexers.Get("bash")})
}

func (l shellLexer) Tokenise(options *chroma.TokeniseOptions, text string) (chroma.Iterator, error) {
	it, err := l.Lexer.Tokenise(options, text)
	if err != nil {
		return nil, err
	}
	tokens := it.Tokens()
	cmdPos, assign := true, false
	for i, tok := range tokens {
		space := strings.TrimSpace(tok.Value) == ""
		switch {
		case tok.Value == "":
		case strings.Contains(tok.Value, "\n"):
			cmdPos, assign = true, false
		case assign:
			// An env assignment (FOO=1 cmd) keeps the next word a command.
			assign = !space
		case space:
		case cmdStarters[tok.Value]:
			cmdPos = true
		case tok.Type == chroma.NameVariable && cmdPos:
			assign = true
		case tok.Type != chroma.Text:
			cmdPos = false
		case cmdPos:
			tokens[i].Type = chroma.NameFunction
			cmdPos = cmdPrefixes[tok.Value]
		case strings.HasPrefix(tok.Value, "-"):
			tokens[i].Type = chroma.NameAttribute
		}
	}
	return chroma.Literator(tokens...), nil
}
