//go:build darwin || linux

package cli

import (
	"os"

	"golang.org/x/sys/unix"
)

// termRaw puts the terminal in raw mode: keys come as they are pressed,
// unechoed, Ctrl-C included. Output keeps its line endings. It returns
// what puts the terminal back.
func termRaw(in, out *os.File) (restore func(), err error) {
	fd := int(in.Fd())
	t, err := unix.IoctlGetTermios(fd, ioctlGetTermios)
	if err != nil {
		return nil, err
	}
	old := *t
	t.Iflag &^= unix.IGNBRK | unix.BRKINT | unix.PARMRK | unix.ISTRIP | unix.INLCR | unix.IGNCR | unix.ICRNL | unix.IXON
	t.Lflag &^= unix.ECHO | unix.ECHONL | unix.ICANON | unix.ISIG | unix.IEXTEN
	t.Cflag &^= unix.CSIZE | unix.PARENB
	t.Cflag |= unix.CS8
	t.Cc[unix.VMIN], t.Cc[unix.VTIME] = 1, 0
	if err := unix.IoctlSetTermios(fd, ioctlSetTermios, t); err != nil {
		return nil, err
	}
	return func() { _ = unix.IoctlSetTermios(fd, ioctlSetTermios, &old) }, nil
}

// termSize is the terminal's size in columns and rows.
func termSize(out *os.File) (w, h int) {
	ws, err := unix.IoctlGetWinsize(int(out.Fd()), unix.TIOCGWINSZ)
	if err != nil || ws.Col == 0 || ws.Row == 0 {
		return 80, 24
	}
	return int(ws.Col), int(ws.Row)
}
