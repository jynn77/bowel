package qr

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
)

// screenTools are the programs that can write a picture of the whole
// screen to a file without asking anything, each with its arguments
// before the file's name. Linux has no one way: Wayland compositors let
// only their own tool look at the screen (grim on wlroots, the desktop's
// on GNOME and KDE), and X11 lets any.
var screenTools = [][]string{
	{"grim"},
	{"spectacle", "-b", "-n", "-f", "-o"},
	{"gnome-screenshot", "-f"},
	{"maim"},
	{"scrot", "-o"},
	{"import", "-silent", "-window", "root"},
}

// capture takes one picture of all the displays together with the first
// tool that is installed and works in this session.
func capture(dir string) ([]string, error) {
	file := filepath.Join(dir, "screen.png")
	found := false
	var last error
	for _, tool := range screenTools {
		bin, err := exec.LookPath(tool[0])
		if err != nil {
			continue
		}
		found = true
		_ = os.Remove(file)
		if last = exec.Command(bin, append(tool[1:], file)...).Run(); last == nil {
			if st, err := os.Stat(file); err == nil && st.Size() > 0 {
				return []string{file}, nil
			}
			last = errors.New("qr: the screenshot tool wrote nothing")
		}
	}
	if !found {
		return nil, ErrNoScreenTool
	}
	return []string{file}, last
}
