// Package cli is bowel's command line: `bowel status`, `bowel mode global`,
// `bowel logs -f` and the rest. It parses the arguments, asks the running
// bowel (the app, or `bowel daemon`) over its command socket, and prints
// the answer for people or, with --json, for programs. It holds no logic
// of the product: every command is a call of a tool the app serves
// (app_cli.go, app_mcp.go in the main package).
//
// Exit codes: 0 done, 1 it failed (bowel isn't running, the app refused),
// 2 the command line was wrong.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/oboo/moho/internal/i18n"
)

// Exit codes.
const (
	ExitOK    = 0
	ExitFail  = 1
	ExitUsage = 2
)

// Env is what a run of the command line is given.
type Env struct {
	Args           []string
	Stdout, Stderr io.Writer
	// TTY: Stdout is a terminal, so colors and progress may be shown.
	TTY    bool
	Getenv func(string) string
	// Socket is the running bowel's command socket.
	Socket  string
	Version string
	// Local are the commands that run in this process, by name: the main
	// package's daemon, mcp and devtools.
	Local map[string]func(args []string) error
	// Password asks for a password at the terminal, unechoed; nil is the
	// terminal's own, where there is one.
	Password func(prompt string) (string, error)
	// Context ends the commands that go on (logs -f, conns --watch); Poll
	// is how often they ask. Both have defaults; tests set them.
	Context context.Context
	Poll    time.Duration
}

// words are a few words in Chinese and English, put in the interface's
// language when they are shown (the other languages are i18n's tables).
type words struct{ zh, en string }

func (w words) String() string { return i18n.T(w.zh, w.en) }

// Command is one command, or a group of them.
type Command struct {
	Name    string
	Aliases []string
	// Args is how the arguments are written in the usage line.
	Args  string
	Short words
	// Examples are whole command lines.
	Examples []string
	Flags    []Flag
	Sub      []*Command
	// Default is the subcommand a group runs when none is named.
	Default string
	// Min and Max bound the arguments; Max < 0 is any number.
	Min, Max int
	// Choices are what the first argument may be, when it is one of a few.
	Choices []string
	// Raw commands take their arguments as they are (Env.Local runs them).
	Raw bool
	Run func(c *Ctx) error
}

// Flag is an option of a command.
type Flag struct {
	Name  string // --name
	Short string // -n
	// Kind: "bool", "string" or "int".
	Kind    string
	Help    words
	Choices []string
}

var globalFlags = []Flag{
	{Name: "json", Kind: "bool", Help: words{"输出 JSON，供脚本读取", "Print JSON, for scripts"}},
	{Name: "no-color", Kind: "bool", Help: words{"不使用颜色", "Don't use colors"}},
	{Name: "help", Short: "h", Kind: "bool", Help: words{"显示帮助", "Show help"}},
}

// Ctx is a command being run.
type Ctx struct {
	env   Env
	root  *Command
	path  []*Command
	args  []string
	flags map[string]string
	json  bool
	color bool
	out   io.Writer
	errw  io.Writer
	cl    *client
}

func (c *Ctx) cmd() *Command { return c.path[len(c.path)-1] }

func (c *Ctx) flag(name string) string { return c.flags[name] }
func (c *Ctx) has(name string) bool    { return c.flags[name] == "true" }
func (c *Ctx) intFlag(name string, def int) int {
	if v, ok := c.flags[name]; ok {
		n, _ := strconv.Atoi(v)
		return n
	}
	return def
}

func (c *Ctx) arg(i int) string {
	if i < len(c.args) {
		return c.args[i]
	}
	return ""
}

// usageError is a wrong command line: exit code 2, with the way to help.
type usageError struct {
	msg  string
	path []*Command
}

func (e *usageError) Error() string { return e.msg }

func usagef(path []*Command, format string, a ...any) error {
	return &usageError{msg: fmt.Sprintf(format, a...), path: path}
}

// errHelp asks for the help of the command parsed so far.
var errHelp = errors.New("help")

// WantsGUI says whether these arguments open the app, not a command: none,
// or what a launcher passes (macOS's -psn_…, a URL).
func WantsGUI(args []string) bool {
	if len(args) == 0 {
		return true
	}
	return strings.HasPrefix(args[0], "-psn_") || strings.Contains(args[0], "://")
}

// DataDir takes --data-dir out of args: the main package needs the data
// folder before the command is run.
func DataDir(args []string) (rest []string, dir string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			return append(rest, args[i:]...), dir
		case a == "--data-dir" && i+1 < len(args):
			dir = args[i+1]
			i++
		case strings.HasPrefix(a, "--data-dir="):
			dir = strings.TrimPrefix(a, "--data-dir=")
		default:
			rest = append(rest, a)
		}
	}
	return rest, dir
}

// IsTerminal says whether f is a terminal.
func IsTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// Run runs the command line and returns the exit code.
func Run(env Env) int {
	if env.Getenv == nil {
		env.Getenv = func(string) string { return "" }
	}
	if env.Context == nil {
		env.Context = context.Background()
	}
	if env.Poll == 0 {
		env.Poll = 700 * time.Millisecond
	}
	root := tree()
	c := &Ctx{env: env, root: root, out: env.Stdout, errw: env.Stderr, flags: map[string]string{}}
	err := parse(root, env.Args, c)
	c.json = c.has("json")
	c.color = env.TTY && !c.json && !c.has("no-color") && env.Getenv("NO_COLOR") == "" && env.Getenv("TERM") != "dumb"
	if err == nil && c.cmd().Run == nil { // a group with nothing to run by itself
		err = errHelp
	}
	if err == nil {
		err = c.cmd().Run(c)
	}
	if c.cl != nil {
		c.cl.close()
	}
	var ue *usageError
	switch {
	case err == nil:
		return ExitOK
	case errors.Is(err, errHelp):
		writeHelp(c.out, c.path)
		return ExitOK
	case errors.As(err, &ue):
		fmt.Fprintln(c.errw, c.paint(red, "bowel: ")+ue.msg)
		fmt.Fprintf(c.errw, i18n.T("用 `%s --help` 查看用法。", "See `%s --help` for usage.")+"\n", pathName(ue.path))
		return ExitUsage
	}
	fmt.Fprintln(c.errw, c.paint(red, "bowel: ")+err.Error())
	return ExitFail
}

func pathName(path []*Command) string {
	names := make([]string, len(path))
	for i, p := range path {
		names[i] = p.Name
	}
	return strings.Join(names, " ")
}

func (cmd *Command) child(name string) *Command {
	for _, s := range cmd.Sub {
		if s.Name == name || slices.Contains(s.Aliases, name) {
			return s
		}
	}
	return nil
}

func (cmd *Command) names() []string {
	var out []string
	for _, s := range cmd.Sub {
		out = append(out, s.Name)
	}
	return out
}

func findFlag(cmd *Command, tok string) *Flag {
	for _, list := range [][]Flag{cmd.Flags, globalFlags} {
		for i, f := range list {
			if tok == "--"+f.Name || (f.Short != "" && tok == "-"+f.Short) {
				return &list[i]
			}
		}
	}
	return nil
}

func isFlag(tok string) bool {
	if len(tok) < 2 || tok[0] != '-' {
		return false
	}
	_, err := strconv.ParseFloat(tok, 64)
	return err != nil // -1 is an argument
}

// parse reads args into c: the command they name, its flags and arguments.
func parse(root *Command, args []string, c *Ctx) error {
	cur := root
	c.path = []*Command{root}
	help := false
	if len(args) > 0 && args[0] == "help" {
		help, args = true, args[1:]
	}
	for i := 0; i < len(args); i++ {
		tok := args[i]
		if cur.Raw && tok != "--help" && tok != "-h" {
			c.args = append(c.args, tok)
			continue
		}
		if tok == "--" {
			c.args = append(c.args, args[i+1:]...)
			break
		}
		if isFlag(tok) {
			name, val, hasVal := strings.Cut(tok, "=")
			f := findFlag(cur, name)
			if f == nil {
				msg := fmt.Sprintf(i18n.T("未知选项 %s", "Unknown option %s"), name)
				var all []string
				for _, f := range append(slices.Clone(cur.Flags), globalFlags...) {
					all = append(all, "--"+f.Name)
				}
				if s := suggest(name, all); s != "" {
					msg += fmt.Sprintf(i18n.T("。你是不是想用 %s？", ". Did you mean %s?"), s)
				}
				return &usageError{msg: msg, path: c.path}
			}
			switch {
			case f.Kind == "bool" && !hasVal:
				val = "true"
			case f.Kind != "bool" && !hasVal:
				if i+1 >= len(args) {
					return usagef(c.path, i18n.T("选项 %s 需要一个值", "Option %s needs a value"), name)
				}
				i++
				val = args[i]
			}
			if f.Kind == "int" {
				if n, err := strconv.Atoi(val); err != nil || n < 0 {
					return usagef(c.path, i18n.T("选项 %s 要一个数字，不是 %s", "Option %s takes a number, not %s"), name, val)
				}
			}
			if len(f.Choices) > 0 && !slices.Contains(f.Choices, val) {
				return usagef(c.path, i18n.T("选项 %s 只能是 %s，不是 %s", "Option %s is one of %s, not %s"), name, strings.Join(f.Choices, ", "), val)
			}
			c.flags[f.Name] = val
			continue
		}
		if len(cur.Sub) > 0 && len(c.args) == 0 {
			next := cur.child(tok)
			if next == nil {
				msg := fmt.Sprintf(i18n.T("未知命令 %s", "Unknown command %s"), tok)
				if s := suggest(tok, cur.names()); s != "" {
					msg += fmt.Sprintf(i18n.T("。你是不是想用 %s？", ". Did you mean %s?"), s)
				}
				return &usageError{msg: msg, path: c.path}
			}
			cur = next
			c.path = append(c.path, cur)
			continue
		}
		c.args = append(c.args, tok)
	}
	if help || c.has("help") {
		return errHelp
	}
	if cur.Run == nil && cur.Default != "" {
		cur = cur.child(cur.Default)
		c.path = append(c.path, cur)
	}
	if cur.Run == nil || cur.Raw {
		return nil
	}
	switch {
	case len(c.args) < cur.Min:
		return usagef(c.path, i18n.T("缺少参数：%s", "Missing arguments: %s"), pathName(c.path)+" "+cur.Args)
	case cur.Max >= 0 && len(c.args) > cur.Max:
		return usagef(c.path, i18n.T("多余的参数：%s", "Too many arguments: %s"), strings.Join(c.args[cur.Max:], " "))
	}
	if len(cur.Choices) > 0 && len(c.args) > 0 && !slices.Contains(cur.Choices, c.args[0]) {
		msg := fmt.Sprintf(i18n.T("%s 只能是 %s，不是 %s", "%s takes %s, not %s"), pathName(c.path), strings.Join(cur.Choices, ", "), c.args[0])
		if s := suggest(c.args[0], cur.Choices); s != "" {
			msg += fmt.Sprintf(i18n.T("。你是不是想用 %s？", ". Did you mean %s?"), s)
		}
		return &usageError{msg: msg, path: c.path}
	}
	return nil
}

// suggest is the candidate nearest to s, "" when none is near.
func suggest(s string, candidates []string) string {
	best, bestD := "", 3
	for _, cand := range candidates {
		d := distance(strings.ToLower(s), strings.ToLower(cand))
		if strings.HasPrefix(cand, s) && len(s) >= 2 {
			d = min(d, 1)
		}
		if d < bestD && d < max(len(cand), 2) {
			best, bestD = cand, d
		}
	}
	return best
}

// distance is the edit distance of a and b, a swap of neighbours being one.
func distance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	d := make([][]int, len(ra)+1)
	for i := range d {
		d[i] = make([]int, len(rb)+1)
		d[i][0] = i
	}
	for j := range d[0] {
		d[0][j] = j
	}
	for i := 1; i <= len(ra); i++ {
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			d[i][j] = min(d[i-1][j]+1, d[i][j-1]+1, d[i-1][j-1]+cost)
			if i > 1 && j > 1 && ra[i-1] == rb[j-2] && ra[i-2] == rb[j-1] {
				d[i][j] = min(d[i][j], d[i-2][j-2]+1)
			}
		}
	}
	return d[len(ra)][len(rb)]
}

// writeHelp prints the help of the command at the end of path.
func writeHelp(w io.Writer, path []*Command) {
	cmd := path[len(path)-1]
	fmt.Fprintln(w, cmd.Short.String())
	fmt.Fprintln(w)
	fmt.Fprintln(w, i18n.T("用法：", "Usage:"))
	line := "  " + pathName(path)
	if len(cmd.Sub) > 0 {
		line += " " + i18n.T("<命令>", "<command>")
	}
	if cmd.Args != "" {
		line += " " + cmd.Args
	}
	fmt.Fprintln(w, line+" "+i18n.T("[选项]", "[options]"))
	if len(cmd.Sub) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, i18n.T("命令：", "Commands:"))
		var rows [][]string
		for _, s := range cmd.Sub {
			name := s.Name
			if s.Args != "" && len(s.Sub) == 0 {
				name += " " + s.Args
			}
			rows = append(rows, []string{name, s.Short.String()})
		}
		writeColumns(w, rows)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, i18n.T("选项：", "Options:"))
	var rows [][]string
	flags := append(slices.Clone(cmd.Flags), globalFlags...)
	if len(path) == 1 {
		flags = append(flags, Flag{Name: "data-dir", Kind: "string", Help: words{"使用另一个数据目录（也可用环境变量 BOWEL_DATA_DIR）", "Use another data folder (or the environment's BOWEL_DATA_DIR)"}})
	}
	for _, f := range flags {
		name := "--" + f.Name
		if f.Short != "" {
			name = "-" + f.Short + ", " + name
		} else {
			name = "    " + name
		}
		switch {
		case len(f.Choices) > 0:
			name += " " + strings.Join(f.Choices, "|")
		case f.Kind == "int":
			name += " N"
		case f.Kind == "string":
			name += " " + i18n.T("<值>", "<value>")
		}
		rows = append(rows, []string{name, f.Help.String()})
	}
	writeColumns(w, rows)
	if len(cmd.Examples) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, i18n.T("例子：", "Examples:"))
		for _, e := range cmd.Examples {
			fmt.Fprintln(w, "  "+e)
		}
	}
	if len(path) == 1 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, i18n.T("不带命令时打开应用。用 `bowel <命令> --help` 查看某个命令。", "Without a command the app opens. See `bowel <command> --help` for a command."))
	}
}

// writeColumns prints rows of two columns, the second lined up.
func writeColumns(w io.Writer, rows [][]string) {
	width := 0
	for _, r := range rows {
		width = max(width, dispWidth(r[0]))
	}
	for _, r := range rows {
		fmt.Fprintln(w, "  "+pad(r[0], width)+"  "+r[1])
	}
}
