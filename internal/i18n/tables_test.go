package i18n

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode"
)

// notShown are Chinese literals that are names, not words for people:
// they stay Chinese in every language.
var notShown = []string{
	"·自动",
	// matched against a network name by ipinfo: data, not a word for people
	"电信", "联通", "移动", "铁通", "广电", "宽带",
}

// skipFiles hold Chinese that is data or is translated elsewhere
// (geo.Display has its own tables).
var skipFiles = []string{"internal/geo/data.go", "internal/geo/names.go", "internal/i18n/"}

// TestTablesCoverSource: every Chinese string literal in the program has
// Japanese and Traditional Chinese.
func TestTablesCoverSource(t *testing.T) {
	root, _ := filepath.Abs("../..")
	fset := token.NewFileSet()
	missing := map[string][]string{}
	filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		rel, _ := filepath.Rel(root, path)
		// skipFiles are written with '/', and Rel answers with '\\' on Windows:
		// without this, nothing there is skipped and geo/data.go alone reports
		// every one of its place names.
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") || d.Name() == "prototype" || d.Name() == "resources" || d.Name() == "scripts" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") ||
			slices.ContainsFunc(skipFiles, func(s string) bool { return strings.HasPrefix(rel, s) }) {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Error(err)
			return nil
		}
		ast.Inspect(f, func(n ast.Node) bool {
			l, ok := n.(*ast.BasicLit)
			if !ok || l.Kind != token.STRING {
				return true
			}
			s, err := strconv.Unquote(l.Value)
			if err != nil || !hasHan(s) || slices.Contains(notShown, s) {
				return true
			}
			where := fset.Position(l.Pos()).String()
			where = strings.TrimPrefix(where, root+"/")
			if _, ok := jaTable[s]; !ok {
				missing["ja"] = append(missing["ja"], where+" "+strconv.Quote(s))
			}
			if _, ok := hantTable[s]; !ok {
				missing["zh-Hant"] = append(missing["zh-Hant"], where+" "+strconv.Quote(s))
			}
			return true
		})
		return nil
	})
	for lang, m := range missing {
		t.Errorf("%d strings lack %s (add them to its table):\n%s", len(m), lang, strings.Join(m, "\n"))
	}
}

var verb = regexp.MustCompile(`%(?:\[\d+\])?[-+# 0]*\d*(?:\.\d+)?[a-zA-Z%]|\{n\}`)

// verbs are s's format verbs, without their indexes, sorted.
func verbs(s string) []string {
	v := verb.FindAllString(s, -1)
	for i := range v {
		v[i] = regexp.MustCompile(`\[\d+\]`).ReplaceAllString(v[i], "")
	}
	slices.Sort(v)
	return v
}

// TestTablesKeepVerbs: a translation formats the same arguments as its
// Chinese.
func TestTablesKeepVerbs(t *testing.T) {
	for name, table := range map[string]map[string]string{"ja": jaTable, "zh-Hant": hantTable} {
		for zh, s := range table {
			if s == "" {
				t.Errorf("%s: %q is empty", name, zh)
			}
			if !slices.Equal(verbs(zh), verbs(s)) {
				t.Errorf("%s: %q formats %v, %q %v", name, zh, verbs(zh), s, verbs(s))
			}
		}
	}
}

func TestLanguages(t *testing.T) {
	defer Set("zh")
	for _, l := range Languages {
		if !Known(l) || Names[l] == "" {
			t.Errorf("%s has no name", l)
		}
		Set(l)
		if Lang() != l {
			t.Errorf("Set(%s) gives %s", l, Lang())
		}
	}
	Set("fr")
	if Lang() != "zh" {
		t.Errorf("an unknown language gives %s", Lang())
	}
	Set("ja")
	if got := T("没有这条", "Not there"); got != "Not there" {
		t.Errorf("Japanese without a translation is %q, want English", got)
	}
	Set("zh-Hant")
	if got := T("没有这条", "Not there"); got != "没有这条" {
		t.Errorf("Traditional without a translation is %q, want Simplified", got)
	}
}

func hasHan(s string) bool {
	return strings.ContainsFunc(s, func(r rune) bool { return unicode.Is(unicode.Han, r) })
}
