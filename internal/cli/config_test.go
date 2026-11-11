package cli

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// TestConfigCommands: the file's path goes to the app whole, the password
// comes from the environment or the terminal and never from the command
// line, and a file that needs one is asked about once.
func TestConfigCommands(t *testing.T) {
	var got []map[string]any
	needs := "" // the password the file to import has
	socket, calls := fake(t, map[string]func(map[string]any) (any, error){
		"config_export": func(a map[string]any) (any, error) {
			got = append(got, a)
			return map[string]any{"path": a["path"], "encrypted": a["password"] != ""}, nil
		},
		"config_import": func(a map[string]any) (any, error) {
			got = append(got, a)
			if a["undo"] != true && a["password"] != needs {
				return map[string]any{"imported": false, "needPassword": true, "message": "Wrong password"}, nil
			}
			return map[string]any{"imported": true}, nil
		},
	})
	var asked []string
	typed := []string{}
	env := Env{Socket: socket, Password: func(prompt string) (string, error) {
		asked = append(asked, prompt)
		if len(typed) == 0 {
			return "", errors.New("nothing typed")
		}
		p := typed[0]
		typed = typed[1:]
		return p, nil
	}}
	reset := func() { got, asked, *calls = nil, nil, nil }
	abs, _ := filepath.Abs("c.bowel")

	// No password: the file is plain, and the command says so.
	code, out, errOut := run(t, env, "config", "export", "c.bowel")
	if code != 0 || len(got) != 1 || got[0]["path"] != abs || got[0]["password"] != "" || len(asked) != 0 {
		t.Fatalf("export: %d %v %q", code, got, errOut)
	}
	if !strings.Contains(out, "Exported to "+abs) || !strings.Contains(errOut, "not encrypted") {
		t.Errorf("export said %q %q", out, errOut)
	}

	// --password asks twice, and the two must agree.
	reset()
	typed = []string{"pw", "pw"}
	if code, _, errOut = run(t, env, "config", "export", "c.bowel", "--password"); code != 0 || len(asked) != 2 || got[0]["password"] != "pw" || strings.Contains(errOut, "not encrypted") {
		t.Fatalf("export --password: %d %v %v %q", code, got, asked, errOut)
	}
	reset()
	typed = []string{"pw", "pv"}
	if code, _, _ = run(t, env, "config", "export", "c.bowel", "-p"); code == 0 || len(got) != 0 {
		t.Fatalf("passwords that differ: %d %v", code, got)
	}
	reset()
	typed = []string{"", ""}
	if code, _, _ = run(t, env, "config", "export", "c.bowel", "-p"); code == 0 || len(got) != 0 {
		t.Fatalf("an empty password: %d %v", code, got)
	}

	// The environment's password needs no terminal.
	reset()
	withPW := env
	withPW.Getenv = func(k string) string {
		if k == passwordEnv {
			return "envpw"
		}
		return ""
	}
	if code, _, _ = run(t, withPW, "config", "export", "c.bowel"); code != 0 || got[0]["password"] != "envpw" || len(asked) != 0 {
		t.Fatalf("export with %s: %d %v", passwordEnv, code, got)
	}

	// Import: a plain file is not asked about; a sealed one once.
	reset()
	if code, out, _ = run(t, env, "config", "import", "c.bowel"); code != 0 || len(got) != 1 || got[0]["path"] != abs || len(asked) != 0 || !strings.Contains(out, "Imported") {
		t.Fatalf("import: %d %v %q", code, got, out)
	}
	reset()
	needs, typed = "pw", []string{"pw"}
	if code, out, _ = run(t, env, "config", "import", "c.bowel"); code != 0 || len(got) != 2 || len(asked) != 1 || got[1]["password"] != "pw" || !strings.Contains(out, "Imported") {
		t.Fatalf("import of a sealed file: %d %v %v", code, got, asked)
	}
	reset()
	typed = []string{"wrong"}
	if code, _, errOut = run(t, env, "config", "import", "c.bowel"); code == 0 || len(got) != 2 || !strings.Contains(errOut, "Wrong password") {
		t.Fatalf("a wrong password: %d %v %q", code, got, errOut)
	}
	reset()
	if code, _, errOut = run(t, withPW, "config", "import", "c.bowel"); code == 0 || len(got) != 1 || len(asked) != 0 || !strings.Contains(errOut, "Wrong password") {
		t.Fatalf("a wrong password from the environment: %d %v %q", code, got, errOut)
	}

	// --undo names no file.
	reset()
	needs = ""
	if code, out, _ = run(t, env, "config", "import", "--undo"); code != 0 || got[0]["undo"] != true || got[0]["path"] != nil || !strings.Contains(out, "Undid") {
		t.Fatalf("undo: %d %v %q", code, got, out)
	}
	reset()
	for _, args := range [][]string{{"config", "import"}, {"config", "import", "c.bowel", "--undo"}, {"config", "export"}} {
		if code, _, _ = run(t, env, args...); code == 0 || len(got) != 0 {
			t.Errorf("%v: %d %v", args, code, got)
		}
	}
}
