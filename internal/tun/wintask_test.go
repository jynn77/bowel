package tun

import (
	"encoding/xml"
	"errors"
	"strings"
	"testing"
	"unicode/utf16"
)

// The task runs the protected copy with the app's own arguments, as the
// user, elevated, with no trigger; and the definition is well-formed XML
// whatever the paths hold.
func TestTaskXML(t *testing.T) {
	x := taskXML("S-1-5-21-1-2-3-1001", `C:\ProgramData\bowel\tun\S-1-5-21-1-2-3-1001\mihomo.exe`, `C:\Users\张 & 三\AppData\Roaming\bowel\core`)
	var task struct {
		Principals struct {
			Principal struct {
				UserId    string
				LogonType string
				RunLevel  string
			}
		}
		Triggers struct {
			Inner string `xml:",innerxml"`
		}
		Settings struct {
			ExecutionTimeLimit         string
			AllowStartOnDemand         bool
			DisallowStartIfOnBatteries bool
			StopIfGoingOnBatteries     bool
			MultipleInstancesPolicy    string
		}
		Actions struct {
			Exec struct {
				Command          string
				Arguments        string
				WorkingDirectory string
			}
		}
	}
	// The declaration names the file's encoding, not this string's.
	body := strings.TrimPrefix(x, `<?xml version="1.0" encoding="UTF-16"?>`)
	if body == x {
		t.Fatal("no UTF-16 declaration")
	}
	if err := xml.Unmarshal([]byte(body), &task); err != nil {
		t.Fatalf("not XML: %v\n%s", err, x)
	}
	p := task.Principals.Principal
	if p.UserId != "S-1-5-21-1-2-3-1001" || p.LogonType != "S4U" || p.RunLevel != "HighestAvailable" {
		t.Errorf("principal %+v", p)
	}
	e := task.Actions.Exec
	if e.Command != `C:\ProgramData\bowel\tun\S-1-5-21-1-2-3-1001\mihomo.exe` ||
		e.Arguments != `-d "C:\Users\张 & 三\AppData\Roaming\bowel\core" -f "C:\Users\张 & 三\AppData\Roaming\bowel\core\config.yaml"` ||
		e.WorkingDirectory != `C:\Users\张 & 三\AppData\Roaming\bowel\core` {
		t.Errorf("action %+v", e)
	}
	s := task.Settings
	if s.ExecutionTimeLimit != "PT0S" || !s.AllowStartOnDemand || s.DisallowStartIfOnBatteries || s.StopIfGoingOnBatteries || s.MultipleInstancesPolicy != "IgnoreNew" {
		t.Errorf("settings %+v", s)
	}
	if strings.TrimSpace(task.Triggers.Inner) != "" {
		t.Errorf("the task has a trigger: %q", task.Triggers.Inner)
	}
}

func TestUTF16File(t *testing.T) {
	b := utf16File("a\n张")
	if len(b) != 2+2*4 || b[0] != 0xff || b[1] != 0xfe {
		t.Fatalf("bytes % x", b)
	}
	u := make([]uint16, 0, 4)
	for i := 2; i < len(b); i += 2 {
		u = append(u, uint16(b[i])|uint16(b[i+1])<<8)
	}
	if got := string(utf16.Decode(u)); got != "a\r\n张" {
		t.Errorf("decoded %q", got)
	}
}

// The elevated command closes the directory before anything is put in
// it, moves a copy in use aside instead of writing over it, and leaves
// the mark last, once everything else has worked. The launcher goes in
// first, and ends what still runs from the core's path before the core is
// replaced; neither that nor ending the tasks can fail the step.
func TestWinInstallStep(t *testing.T) {
	dst := `C:\ProgramData\bowel\tun\S-1\mihomo.exe`
	st := winInstallStep(`C:\Users\a b\Temp\bowel-tun\.mihomo-1`, `C:\Users\a b\Temp\bowel-tun\.mihomo-2`, dst, `C:\Users\a b\Temp\bowel-tun\.mihomo-task.xml`, "bowel-tun-S-1",
		`C:\Users\a b\Temp\bowel-tun\.mihomo-task-interactive.xml`, "bowel-tun-S-1-interactive", "abcd")
	if len(st)%2 != 0 || st[len(st)-1] != `C:\ProgramData\bowel\tun\S-1\installed` {
		t.Fatalf("step doesn't end on the mark: %q", st)
	}
	got, err := winScript([]Step{st})
	if err != nil {
		t.Fatal(err)
	}
	if st[1] != `C:\Users\a b\Temp\bowel-tun\.mihomo-1` {
		t.Errorf("the copy is not the first path: %q", st[1])
	}
	want := `(if not exist "C:\Users\a b\Temp\bowel-tun\.mihomo-1" exit /b 1)` +
		` && (if not exist "C:\ProgramData\bowel\tun\S-1" mkdir "C:\ProgramData\bowel\tun\S-1")` +
		` && icacls "C:\ProgramData\bowel\tun\S-1" /setowner *S-1-5-32-544 /T /C >nul` +
		` && icacls "C:\ProgramData\bowel\tun\S-1" /inheritance:r /grant:r *S-1-5-32-544:(OI)(CI)F *S-1-5-18:(OI)(CI)F *S-1-5-32-545:(OI)(CI)RX >nul` +
		` && (schtasks /End /TN "bowel-tun-S-1" >nul 2>nul & schtasks /End /TN "bowel-tun-S-1-interactive" >nul 2>nul & ver >nul)` +
		` && (del /f /q "C:\ProgramData\bowel\tun\S-1\bowel-tun.exe.old" 2>nul & ver >nul)` +
		` && (if exist "C:\ProgramData\bowel\tun\S-1\bowel-tun.exe" move /y "C:\ProgramData\bowel\tun\S-1\bowel-tun.exe" "C:\ProgramData\bowel\tun\S-1\bowel-tun.exe.old" >nul)` +
		` && copy /y /b "C:\Users\a b\Temp\bowel-tun\.mihomo-2" "C:\ProgramData\bowel\tun\S-1\bowel-tun.exe" >nul` +
		` && (del /f /q "C:\Users\a b\Temp\bowel-tun\.mihomo-2" 2>nul & start "" /wait "C:\ProgramData\bowel\tun\S-1\bowel-tun.exe" tun-core --kill "C:\ProgramData\bowel\tun\S-1\mihomo.exe" & ver >nul)` +
		` && (del /f /q "C:\ProgramData\bowel\tun\S-1\mihomo.exe.old" 2>nul & ver >nul)` +
		` && (if exist "C:\ProgramData\bowel\tun\S-1\mihomo.exe" move /y "C:\ProgramData\bowel\tun\S-1\mihomo.exe" "C:\ProgramData\bowel\tun\S-1\mihomo.exe.old" >nul)` +
		` && copy /y /b "C:\Users\a b\Temp\bowel-tun\.mihomo-1" "C:\ProgramData\bowel\tun\S-1\mihomo.exe" >nul` +
		` && schtasks /Create /F /TN "bowel-tun-S-1" /XML "C:\Users\a b\Temp\bowel-tun\.mihomo-task.xml" >nul` +
		` && schtasks /Create /F /TN "bowel-tun-S-1-interactive" /XML "C:\Users\a b\Temp\bowel-tun\.mihomo-task-interactive.xml" >nul` +
		` && (echo abcd)> "C:\ProgramData\bowel\tun\S-1\installed"`
	if got != want {
		t.Fatalf("script:\n got %s\nwant %s", got, want)
	}
	// Only administrators and SYSTEM get more than reading and running.
	if strings.Count(dirACL, "F") != 2 || !strings.Contains(dirACL, "*S-1-5-32-545:(OI)(CI)RX") {
		t.Errorf("ACL %s", dirACL)
	}
}

func TestWinScriptQuoting(t *testing.T) {
	s, err := winScript([]Step{{"del ", `C:\a b\c&d^e`}, {"echo ok"}})
	if err != nil || s != `del "C:\a b\c&d^e" && echo ok` {
		t.Fatalf("script %q, %v", s, err)
	}
	for _, bad := range []string{`C:\a"b`, `C:\%TEMP%\x`, "C:\\a\nb"} {
		if _, err := winScript([]Step{{"del ", bad}}); err == nil {
			t.Errorf("%q was taken", bad)
		}
	}
}

// The mark changes with anything the task was registered with, and not
// with the case of a path.
func TestMark(t *testing.T) {
	m := mark(`C:\P\mihomo.exe`, `C:\U\core`, "S-1")
	if len(m) != 16 || m != mark(`c:\p\MIHOMO.exe`, `C:\u\core`, "S-1") {
		t.Errorf("mark %q", m)
	}
	for _, o := range []string{mark(`C:\P2\mihomo.exe`, `C:\U\core`, "S-1"), mark(`C:\P\mihomo.exe`, `C:\U2\core`, "S-1"), mark(`C:\P\mihomo.exe`, `C:\U\core`, "S-2")} {
		if o == m {
			t.Error("the mark is the same for another installation")
		}
	}
}

// The task's core gets no environment from the app, and mihomo reads no
// file outside its -d without one: the action is the core alone, in the
// directory the config's files are kept in, with no shell around it that
// ending the task would end in the core's place.
func TestTaskRunsTheCoreItself(t *testing.T) {
	bin := `C:\ProgramData\bowel\tun\S-1\mihomo.exe`
	x := taskXML("S-1", bin, `C:\Users\u\AppData\Roaming\bowel\core`)
	if !strings.Contains(x, "<Command>"+bin+"</Command>") {
		t.Errorf("the action is not the core:\n%s", x)
	}
	if strings.Contains(strings.ToLower(x), "cmd") || strings.Contains(x, "SAFE_PATHS") {
		t.Errorf("the action goes through a shell:\n%s", x)
	}
}

// What the description tells the user to delete is all of the
// installation: the task alone leaves the mark, and Installed true.
func TestTaskDescriptionNamesTheFolder(t *testing.T) {
	x := taskXML("S-1", `C:\ProgramData\bowel\tun\S & 1\mihomo.exe`, `C:\core`)
	var task struct {
		RegistrationInfo struct{ Description string }
	}
	if err := xml.Unmarshal([]byte(strings.TrimPrefix(x, `<?xml version="1.0" encoding="UTF-16"?>`)), &task); err != nil {
		t.Fatal(err)
	}
	d := task.RegistrationInfo.Description
	if !strings.Contains(d, "delete this task and the folder "+`C:\ProgramData\bowel\tun\S & 1`+", and the task bowel-tun-S-1-interactive") || strings.Contains(d, "mihomo.exe") {
		t.Errorf("description %q", d)
	}
	// The second task's names the first.
	x = fallbackTaskXML("S-1", `C:\ProgramData\bowel\tun\S & 1\bowel-tun.exe`, `C:\ProgramData\bowel\tun\S & 1\mihomo.exe`, `C:\core`)
	if err := xml.Unmarshal([]byte(strings.TrimPrefix(x, `<?xml version="1.0" encoding="UTF-16"?>`)), &task); err != nil {
		t.Fatal(err)
	}
	if d := task.RegistrationInfo.Description; !strings.HasSuffix(d, "delete this task and the folder "+`C:\ProgramData\bowel\tun\S & 1`+", and the task bowel-tun-S-1") {
		t.Errorf("the second task's description %q", d)
	}
	if markPath(`C:\ProgramData\bowel\tun\S & 1\mihomo.exe`) != `C:\ProgramData\bowel\tun\S & 1\installed` {
		t.Error("the mark is not in the folder the description names")
	}
}

// A task that did not run because the Task Scheduler no longer has it is
// an installation that is gone; any other failure is reported as it is.
func TestTaskRunErr(t *testing.T) {
	run := errors.New("exit status 1: ERROR")
	if err := taskRunErr(run, errors.New("exit status 1")); !errors.Is(err, ErrGone) {
		t.Errorf("deleted task: %v", err)
	}
	err := taskRunErr(run, nil)
	if errors.Is(err, ErrGone) || !strings.Contains(err.Error(), "exit status 1: ERROR") {
		t.Errorf("task that is there: %v", err)
	}
}

// The second task differs from the first in two things only: it runs in
// the session the user is logged on in, which takes no logon and so no
// domain controller, and the core is started by the launcher kept beside
// it, so that it has no window there and ends when the task is ended. The
// console host run headless, which this replaces, left the core running.
func TestFallbackTaskXML(t *testing.T) {
	sid, bin, dir := "S-1-5-21-1-2-3-1001", `C:\ProgramData\bowel\tun\S-1-5-21-1-2-3-1001\mihomo.exe`, `C:\Users\张 & 三\AppData\Roaming\bowel\core`
	launcher := launcherPath(bin)
	if launcher != `C:\ProgramData\bowel\tun\S-1-5-21-1-2-3-1001\bowel-tun.exe` {
		t.Fatalf("the launcher is not beside the core: %s", launcher)
	}
	x := fallbackTaskXML(sid, launcher, bin, dir)
	if strings.Contains(strings.ToLower(x), "conhost") {
		t.Errorf("the action goes through the console host:\n%s", x)
	}
	var task struct {
		Principals struct {
			Principal struct{ UserId, LogonType, RunLevel string }
		}
		Actions struct {
			Exec struct{ Command, Arguments, WorkingDirectory string }
		}
	}
	if err := xml.Unmarshal([]byte(strings.TrimPrefix(x, `<?xml version="1.0" encoding="UTF-16"?>`)), &task); err != nil {
		t.Fatalf("not XML: %v\n%s", err, x)
	}
	if p := task.Principals.Principal; p.UserId != sid || p.LogonType != "InteractiveToken" || p.RunLevel != "HighestAvailable" {
		t.Errorf("principal %+v", p)
	}
	e := task.Actions.Exec
	if e.Command != launcher || e.WorkingDirectory != dir ||
		e.Arguments != `tun-core "`+bin+`" -d "`+dir+`" -f "`+dir+`\config.yaml"` {
		t.Errorf("action %+v", e)
	}
	// Everything else is the first task's.
	first := taskXML(sid, bin, dir)
	same := strings.NewReplacer(
		"<LogonType>InteractiveToken</LogonType>", "<LogonType>S4U</LogonType>",
		"<Command>"+launcher+"</Command>", "<Command>"+bin+"</Command>",
		`<Arguments>tun-core "`+bin+`" `, "<Arguments>",
		", and the task bowel-tun-"+sid+"</Description>", ", and the task bowel-tun-"+sid+"-interactive</Description>",
	).Replace(x)
	if same != first {
		t.Errorf("the second task differs in more than its logon and its action:\n%s\n%s", x, first)
	}
	if fallbackName(taskNameOf(sid)) != "bowel-tun-"+sid+"-interactive" {
		t.Error("the second task's name")
	}
}

func TestTaskOrder(t *testing.T) {
	for last, want := range map[string]string{
		"":              "t t-interactive",
		"interactive\n": "t-interactive t",
		"garbage":       "t t-interactive",
	} {
		if got := strings.Join(taskOrder("t", last), " "); got != want {
			t.Errorf("after %q: %s, want %s", last, got, want)
		}
	}
}

// Which task starts the core, and what is said when none does.
func TestStartTask(t *testing.T) {
	missing := errors.New("exit status 1: ERROR: The system cannot find the file specified.")
	refused := errors.New("exit status 1: ERROR: refused")
	type task struct {
		run     error // what schtasks /Run says
		absent  bool  // the Task Scheduler does not have it
		started bool  // the core's process appears after it ran
	}
	for _, c := range []struct {
		name            string
		last            string
		first, fallback task
		want            string // the task that started the core
		gone            bool   // the error is ErrGone
		err             string // part of the error's text, in the language tests run in
		ran             string // the tasks run, in order
	}{
		{name: "the first task starts it", first: task{started: true}, fallback: task{started: true},
			want: "t", ran: "t"},
		{name: "a logon that fails starts nothing, and the second task does", first: task{}, fallback: task{started: true},
			want: "t-interactive", ran: "t t-interactive"},
		{name: "the second task first when it was what worked", last: "interactive", first: task{started: true}, fallback: task{started: true},
			want: "t-interactive", ran: "t-interactive"},
		{name: "nobody logged on at the desktop: back to the first", last: "interactive", first: task{started: true}, fallback: task{},
			want: "t", ran: "t-interactive t"},
		{name: "the first task was deleted", first: task{run: missing, absent: true}, fallback: task{started: true},
			gone: true, ran: "t"},
		{name: "an older installation, whose first task starts nothing", first: task{}, fallback: task{run: missing, absent: true},
			gone: true, err: "连不上域控制器", ran: "t t-interactive"},
		{name: "an older installation, whose first task starts it", last: "interactive", first: task{started: true}, fallback: task{run: missing, absent: true},
			want: "t", ran: "t-interactive t"},
		{name: "neither starts it", first: task{}, fallback: task{},
			err: "没有把内核启动起来", ran: "t t-interactive"},
		{name: "the first task's reason is the one given", last: "interactive", first: task{run: refused}, fallback: task{},
			err: "refused", ran: "t-interactive t"},
	} {
		tasks := map[string]task{"t": c.first, "t-interactive": c.fallback}
		var ran []string
		got, err := startTask("t", c.last,
			func(n string) error { ran = append(ran, n); return tasks[n].run },
			func(n string) error {
				if tasks[n].absent {
					return missing
				}
				return nil
			},
			func() bool { return tasks[ran[len(ran)-1]].started })
		if got != c.want || (err == nil) != (c.want != "") {
			t.Errorf("%s: started by %q, %v", c.name, got, err)
			continue
		}
		if strings.Join(ran, " ") != c.ran {
			t.Errorf("%s: ran %v, want %s", c.name, ran, c.ran)
		}
		if err != nil && (errors.Is(err, ErrGone) != c.gone || !strings.Contains(err.Error(), c.err)) {
			t.Errorf("%s: error %v (gone %v)", c.name, err, errors.Is(err, ErrGone))
		}
	}
}

// What follows the launcher's own argument is the core and its arguments,
// as the second task has them, or a path to end the processes of, as the
// installation asks; anything else starts nothing.
func TestLaunchArgs(t *testing.T) {
	bin := `C:\ProgramData\bowel\tun\S-1\mihomo.exe`
	kill, core, rest, ok := launchArgs([]string{bin, "-d", `C:\core`, "-f", `C:\core\config.yaml`})
	if !ok || kill != "" || core != bin || strings.Join(rest, " ") != `-d C:\core -f C:\core\config.yaml` {
		t.Errorf("a core to start: %q %q %q %v", kill, core, rest, ok)
	}
	kill, core, rest, ok = launchArgs([]string{"--kill", bin})
	if !ok || kill != bin || core != "" || rest != nil {
		t.Errorf("a path to end: %q %q %q %v", kill, core, rest, ok)
	}
	for _, bad := range [][]string{nil, {""}, {"--kill"}, {"--kill", ""}, {"--kill", bin, "more"}, {"-d", `C:\core`}} {
		if _, _, _, ok := launchArgs(bad); ok {
			t.Errorf("%q was taken", bad)
		}
	}
	// The task's arguments are the launcher's argument, then these.
	x := fallbackTaskXML("S-1", launcherPath(bin), bin, `C:\core`)
	if !strings.Contains(x, "<Arguments>"+LauncherArg+` "`+bin+`" -d `) {
		t.Errorf("the task does not run the launcher as Launch reads it:\n%s", x)
	}
}

// The installed core is told from another program's of the same name by
// its path, whatever its case, and is still itself once a later
// installation has moved it aside.
func TestSameImage(t *testing.T) {
	bin := `C:\ProgramData\bowel\tun\S-1\mihomo.exe`
	for path, want := range map[string]bool{
		bin: true,
		`c:\programdata\BOWEL\tun\s-1\MIHOMO.EXE`: true,
		bin + ".old": true,
		`C:\Program Files\Clash Verge\mihomo.exe`: false,
		"": false,
	} {
		if sameImage(path, bin) != want {
			t.Errorf("sameImage(%q) is not %v", path, want)
		}
	}
	if sameImage("", "") {
		t.Error("no core is every process")
	}
}

// A core that outlived its task is an installation to make again: that
// is what, elevated, ends it.
func TestOrphanedIsGone(t *testing.T) {
	var err error = orphaned{}
	if !errors.Is(err, ErrGone) || err.Error() == ErrGone.Error() {
		t.Errorf("orphaned: %v", err)
	}
}

// An installation from before the launcher does not count as made.
func TestTaskRev(t *testing.T) {
	if taskRev == "1" {
		t.Error("taskRev is that of the installation whose second task left the core running")
	}
}
