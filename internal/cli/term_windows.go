package cli

import (
	"os"

	"golang.org/x/sys/windows"
)

// termRaw puts the console in raw mode, with keys and output as the
// escape sequences other terminals use. It returns what puts it back.
func termRaw(in, out *os.File) (restore func(), err error) {
	hin, hout := windows.Handle(in.Fd()), windows.Handle(out.Fd())
	var min, mout uint32
	if err := windows.GetConsoleMode(hin, &min); err != nil {
		return nil, err
	}
	if err := windows.GetConsoleMode(hout, &mout); err != nil {
		return nil, err
	}
	raw := min&^(windows.ENABLE_ECHO_INPUT|windows.ENABLE_LINE_INPUT|windows.ENABLE_PROCESSED_INPUT) | windows.ENABLE_VIRTUAL_TERMINAL_INPUT
	if err := windows.SetConsoleMode(hin, raw); err != nil {
		return nil, err
	}
	if err := windows.SetConsoleMode(hout, mout|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING); err != nil {
		_ = windows.SetConsoleMode(hin, min)
		return nil, err
	}
	return func() {
		_ = windows.SetConsoleMode(hin, min)
		_ = windows.SetConsoleMode(hout, mout)
	}, nil
}

// termSize is the console window's size in columns and rows.
func termSize(out *os.File) (w, h int) {
	var info windows.ConsoleScreenBufferInfo
	if err := windows.GetConsoleScreenBufferInfo(windows.Handle(out.Fd()), &info); err != nil {
		return 80, 24
	}
	w, h = int(info.Window.Right-info.Window.Left)+1, int(info.Window.Bottom-info.Window.Top)+1
	if w <= 0 || h <= 0 {
		return 80, 24
	}
	return w, h
}
