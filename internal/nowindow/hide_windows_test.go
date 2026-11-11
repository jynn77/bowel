package nowindow

import (
	"context"
	"os/exec"
	"syscall"
	"testing"
)

func TestHidden(t *testing.T) {
	for name, cmd := range map[string]*exec.Cmd{
		"Command":        Command("x.exe"),
		"CommandContext": CommandContext(context.Background(), "x.exe"),
	} {
		a := cmd.SysProcAttr
		if a == nil || !a.HideWindow || a.CreationFlags&createNoWindow == 0 {
			t.Errorf("%s: SysProcAttr = %+v, want the window hidden", name, a)
		}
	}
}

// Hide adds to the attributes a command already has.
func TestHideKeeps(t *testing.T) {
	const newGroup = 0x00000200 // CREATE_NEW_PROCESS_GROUP
	cmd := exec.Command("x.exe")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: newGroup, CmdLine: "x"}
	Hide(cmd)
	a := cmd.SysProcAttr
	if !a.HideWindow || a.CreationFlags != newGroup|createNoWindow || a.CmdLine != "x" {
		t.Errorf("SysProcAttr = %+v", a)
	}
}
