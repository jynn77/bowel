package nowindow

import (
	"os/exec"
	"syscall"
)

const createNoWindow = 0x08000000 // CREATE_NO_WINDOW

// Hide keeps cmd from opening a console window. It adds to what
// cmd.SysProcAttr already asks for and replaces none of it, so it can
// come before or after other settings. Command and CommandContext have
// already called it.
func Hide(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= createNoWindow
}
