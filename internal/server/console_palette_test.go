package server

import (
	"io/fs"
	"os"
	"regexp"
	"strings"
	"testing"
)

// A colour class whose shade is not in the theme is silently not generated, so the element falls
// back to another rule. This once left selected tiles pale under light text in dark mode
// (dark:bg-brand-950/30 had no brand-950). Every brand shade the console uses must be defined.
func TestEveryBrandShadeTheConsoleUsesIsDefined(t *testing.T) {
	css, err := os.ReadFile("../../web/tailwind.css")
	if err != nil {
		t.Fatal(err)
	}
	defined := map[string]bool{}
	for _, m := range regexp.MustCompile(`--color-brand-(\d+):`).FindAllStringSubmatch(string(css), -1) {
		defined[m[1]] = true
	}
	if len(defined) == 0 {
		t.Fatal("found no brand shades in web/tailwind.css")
	}
	use := regexp.MustCompile(`brand-(\d+)`)
	err = fs.WalkDir(uiFiles, "ui", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".js") {
			return err
		}
		b, err := uiFiles.ReadFile(p)
		if err != nil {
			return err
		}
		for _, m := range use.FindAllStringSubmatch(string(b), -1) {
			if !defined[m[1]] {
				t.Errorf("%s uses brand-%s, which web/tailwind.css does not define", p, m[1])
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
