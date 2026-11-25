package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

var (
	cyrillic = regexp.MustCompile(`\p{Cyrillic}`)
	verbs    = regexp.MustCompile(`%[+.\d]*[a-z%]`)
)

// Every Ukrainian string in the code must have an English counterpart.
func TestCatalogComplete(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if f == "catalog.go" || strings.HasSuffix(f, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			s, _ := strconv.Unquote(lit.Value)
			if !cyrillic.MatchString(s) || s == usageUK || s == "Вентилятор %d" {
				return true
			}
			parts := []string{s}
			if f == "categories.go" { // "Section/Label" references
				parts = strings.SplitN(s, "/", 2)
			}
			if _, ok := catalog[s]; ok {
				return true
			}
			for _, p := range parts {
				if cyrillic.MatchString(p) && trForTest(p) == p {
					t.Errorf("%s: no translation for %q", fset.Position(lit.Pos()), p)
				}
			}
			return true
		})
	}
}

func trForTest(s string) string {
	old := english.Load()
	defer english.Store(old)
	english.Store(true)
	return trName(s)
}

// A translation must keep the format verbs and contain no Cyrillic.
func TestCatalogConsistent(t *testing.T) {
	for uk, en := range catalog {
		if cyrillic.MatchString(en) {
			t.Errorf("Cyrillic in translation %q", en)
		}
		if !slices.Equal(verbs.FindAllString(uk, -1), verbs.FindAllString(en, -1)) {
			t.Errorf("format verbs differ: %q and %q", uk, en)
		}
	}
}
