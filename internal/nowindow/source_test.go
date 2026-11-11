package nowindow

import (
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// allowed are the functions that may start a program with os/exec itself
// in a file that builds for Windows, as "path/from/the/module/root.go:
// function", each with the reason. A program that has to show its console
// window belongs here; so does nothing else.
var allowed = map[string]string{}

// starters are the ways os/exec makes a command.
var starters = map[string]bool{"Command": true, "CommandContext": true, "Cmd": true}

// TestEveryProgramIsStartedHidden reads every Go file of the module that
// builds for Windows and fails when one makes a command with os/exec and
// not with this package: such a program opens a console window there, and
// nothing on the systems the tests run on would show it.
func TestEveryProgramIsStartedHidden(t *testing.T) {
	root := moduleRoot(t)
	self, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	used := map[string]bool{}
	files := 0
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if path == root {
				return nil
			}
			// Hidden directories hold other checkouts of this module, and a
			// directory with a go.mod is another module.
			if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") ||
				name == "testdata" || name == "vendor" || name == "node_modules" || path == self {
				return filepath.SkipDir
			}
			if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		if !buildsForWindows(t, filepath.Dir(path), name) {
			return nil
		}
		files++
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		for _, s := range starts(t, path) {
			key := rel + ": " + s.fn
			if allowed[key] != "" {
				used[key] = true
				continue
			}
			t.Errorf("%s:%d: %s uses exec.%s, which opens a console window on Windows; use nowindow.Command or nowindow.CommandContext (or add %q to allowed, with the reason)",
				rel, s.line, s.fn, s.what, key)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files < 20 {
		t.Fatalf("read %d files under %s, which cannot be the whole module", files, root)
	}
	for key := range allowed {
		if !used[key] {
			t.Errorf("allowed[%q] matches nothing any more; remove it", key)
		}
	}
}

// The scan sees through a renamed import and a command built by hand.
func TestStartsFinds(t *testing.T) {
	const src = `package x

import (
	"context"
	run "os/exec"
)

func a() { run.Command("a").Run() }

type T struct{}

func (T) b() { go func() { run.CommandContext(context.Background(), "b").Run() }() }

var c = &run.Cmd{Path: "c"}

func d() { _, _ = run.LookPath("d") }
`
	path := filepath.Join(t.TempDir(), "x.go")
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, s := range starts(t, path) {
		got = append(got, s.fn+" "+s.what)
	}
	want := []string{"a Command", "T.b CommandContext", "(package level) Cmd"}
	if strings.Join(got, "; ") != strings.Join(want, "; ") {
		t.Errorf("starts = %q, want %q", got, want)
	}
}

func moduleRoot(t *testing.T) string {
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		up := filepath.Dir(dir)
		if up == dir {
			t.Fatal("no go.mod above the test")
		}
		dir = up
	}
}

// buildsForWindows goes by the file's name and its build constraints, for
// both processors bowel is built for.
func buildsForWindows(t *testing.T, dir, name string) bool {
	for _, arch := range []string{"amd64", "arm64"} {
		ctx := build.Default
		ctx.GOOS, ctx.GOARCH, ctx.CgoEnabled = "windows", arch, true
		ctx.BuildTags, ctx.ToolTags = nil, nil
		ok, err := ctx.MatchFile(dir, name)
		if err != nil {
			t.Fatalf("%s: %v", filepath.Join(dir, name), err)
		}
		if ok {
			return true
		}
	}
	return false
}

type start struct {
	fn   string // the function it is in, "T.method" for a method
	what string // Command, CommandContext or Cmd
	line int
}

// starts are the places in a file that make an os/exec command.
func starts(t *testing.T, path string) []start {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	pkg := ""
	for _, imp := range f.Imports {
		if p, _ := strconv.Unquote(imp.Path.Value); p != "os/exec" {
			continue
		}
		pkg = "exec"
		if imp.Name != nil {
			pkg = imp.Name.Name
		}
	}
	if pkg == "" || pkg == "_" {
		return nil
	}
	if pkg == "." {
		t.Fatalf("%s imports os/exec with a dot, which this test cannot follow", path)
	}
	var out []start
	for _, decl := range f.Decls {
		fn := "(package level)"
		if d, ok := decl.(*ast.FuncDecl); ok {
			fn = d.Name.Name
			if d.Recv != nil && len(d.Recv.List) == 1 {
				fn = recvName(d.Recv.List[0].Type) + "." + fn
			}
		}
		ast.Inspect(decl, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if x, ok := sel.X.(*ast.Ident); !ok || x.Name != pkg || !starters[sel.Sel.Name] {
				return true
			}
			// exec.Cmd as a type (a parameter, a field) starts nothing; as
			// a literal it does.
			if sel.Sel.Name == "Cmd" && !isLiteral(decl, sel) {
				return true
			}
			out = append(out, start{fn, sel.Sel.Name, fset.Position(sel.Pos()).Line})
			return true
		})
	}
	return out
}

func recvName(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.StarExpr:
		return recvName(e.X)
	case *ast.IndexExpr:
		return recvName(e.X)
	case *ast.IndexListExpr:
		return recvName(e.X)
	case *ast.Ident:
		return e.Name
	}
	return "?"
}

// isLiteral says whether sel is the type of a composite literal in decl.
func isLiteral(decl ast.Node, sel *ast.SelectorExpr) bool {
	found := false
	ast.Inspect(decl, func(n ast.Node) bool {
		if lit, ok := n.(*ast.CompositeLit); ok && lit.Type == sel {
			found = true
		}
		return !found
	})
	return found
}
