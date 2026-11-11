package tun

import (
	"errors"
	"os/exec"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/oboo/moho/internal/nowindow"
)

// Launch is what the app does when it is run as the second task's
// launcher (see fallbackTaskXML), elevated: args are what followed
// LauncherArg, and what it returns is the exit code.
//
// It starts the core without a console window and waits for it. Ending
// the task ends this process alone, so it first puts itself in a job
// that ends its processes when its last handle is closed: the handle is
// this process's, not inherited by the core, and Windows closes it when
// the task is ended. The core is started in the same job, as every
// process a job's process starts is. Without the job no core is started:
// one that cannot be ended is worse than none, and the app then runs the
// bundled core and says why.
//
// With launcherKill it ends the processes running from a path instead,
// for the installation.
func Launch(args []string) int {
	kill, core, rest, ok := launchArgs(args)
	if !ok {
		return 2
	}
	if kill != "" {
		killImage(kill)
		return 0
	}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 3
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE},
	}
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		return 3
	}
	if err := windows.AssignProcessToJobObject(job, windows.CurrentProcess()); err != nil {
		return 3
	}
	err = nowindow.Command(core, rest...).Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &exit):
		return exit.ExitCode()
	}
	return 4
}

// killImage ends every process whose program is bin (see sameImage), and
// waits a moment for each to be gone: the installation replaces the file
// next.
func killImage(bin string) {
	for _, pid := range running(bin) {
		h, err := windows.OpenProcess(windows.PROCESS_TERMINATE|windows.SYNCHRONIZE, false, uint32(pid))
		if err != nil {
			continue
		}
		if windows.TerminateProcess(h, 1) == nil {
			_, _ = windows.WaitForSingleObject(h, 5000)
		}
		windows.CloseHandle(h)
	}
}
