package server

import (
	"io/fs"
	"strings"
	"testing"
)

// The browser's own append writes null and false out as the words "null" and "false" (a card of the assistant showed
// "null" above its buttons). A part that may be missing goes through h() or append() from lib/dom.js, which skip it.
func TestNoOptionalPartIsGivenToTheBrowsersOwnAppend(t *testing.T) {
	err := fs.WalkDir(uiFiles, "ui", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".js") {
			return err
		}
		b, err := fs.ReadFile(uiFiles, path)
		if err != nil {
			return err
		}
		src := string(b)
		for at := 0; ; {
			i := strings.Index(src[at:], ".append(")
			if i < 0 {
				return nil
			}
			at += i + len(".append(")
			for _, arg := range topLevelArgs(src[at:]) {
				if strings.HasSuffix(arg, ": null") || strings.HasPrefix(arg, "null") {
					t.Errorf("%s:%d: .append is given a part that may be null; use append(el, [...]) from lib/dom.js: %.80s",
						path, 1+strings.Count(src[:at], "\n"), arg)
				}
			}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
}

// topLevelArgs splits the arguments of a call, given the source just after its opening bracket.
func topLevelArgs(s string) []string {
	var args []string
	depth, start := 0, 0
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == '\\' {
				i++
			} else if c == quote {
				quote = 0
			}
		case c == '\'' || c == '"' || c == '`':
			quote = c
		case c == '(' || c == '[' || c == '{':
			depth++
		case c == ')' && depth == 0:
			return append(args, strings.TrimSpace(s[start:i]))
		case c == ')' || c == ']' || c == '}':
			depth--
		case c == ',' && depth == 0:
			args = append(args, strings.TrimSpace(s[start:i]))
			start = i + 1
		}
	}
	return args
}
