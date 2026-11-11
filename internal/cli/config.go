package cli

// bowel config: the configuration to a file and back (the app's
// app_backup.go does the work; this asks for the password and says what
// happened).

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/oboo/moho/internal/i18n"
)

// passwordEnv holds the file's password for scripts. A flag would put it
// in the shell's history and in ps.
const passwordEnv = "BOWEL_BACKUP_PASSWORD"

func configCommand() *Command {
	return &Command{Name: "config", Short: words{"把配置（设置、Provider、规则集）导出到文件，或从文件导入", "Export the configuration (settings, Providers, rule sets) to a file, or import one"}, Sub: []*Command{
		{Name: "export", Args: i18n.T("<文件>", "<file>"), Min: 1, Max: 1,
			Short:    words{"导出配置到一个文件；端口、局域网、TUN 等本机设置不在里面", "Export the configuration to a file; this machine's own settings (ports, LAN, TUN…) are left out"},
			Flags:    []Flag{{Name: "password", Short: "p", Kind: "bool", Help: words{"用密码加密：在终端输入，或取自环境变量 BOWEL_BACKUP_PASSWORD", "Encrypt with a password: typed at the terminal, or taken from BOWEL_BACKUP_PASSWORD"}}},
			Examples: []string{"bowel config export ~/bowel.bowel", "bowel config export ~/bowel.bowel --password"}, Run: runConfigExport},
		{Name: "import", Args: i18n.T("[文件]", "[file]"), Max: 1,
			Short:    words{"用文件里的配置替换现在的；本机设置（端口、局域网、TUN 等）保持不变", "Replace the configuration with a file's; this machine's own settings (ports, LAN, TUN…) stay"},
			Flags:    []Flag{{Name: "undo", Kind: "bool", Help: words{"撤销上次导入", "Undo the last import"}}},
			Examples: []string{"bowel config import ~/bowel.bowel", "bowel config import --undo"}, Run: runConfigImport},
	}}
}

// askPassword reads a password from the terminal.
func (c *Ctx) askPassword(prompt string) (string, error) {
	if c.env.Password != nil {
		return c.env.Password(prompt)
	}
	if !IsTerminal(os.Stdin) || !IsTerminal(os.Stderr) {
		return "", fmt.Errorf(i18n.T("这里不是终端，无法输入密码；请设置环境变量 %s", "This is not a terminal, so no password can be typed; set %s"), passwordEnv)
	}
	return readPassword(os.Stdin, os.Stderr, prompt)
}

// readPassword reads a line with the terminal raw, so that nothing typed
// is shown.
func readPassword(in, out *os.File, prompt string) (string, error) {
	restore, err := termRaw(in, out)
	if err != nil {
		return "", err
	}
	defer restore()
	fmt.Fprint(out, prompt)
	defer fmt.Fprint(out, "\r\n")
	var line []byte
	buf := make([]byte, 1)
	for {
		if n, err := in.Read(buf); err != nil || n == 0 {
			return "", errors.New(i18n.T("没有读到密码", "No password was read"))
		}
		switch b := buf[0]; b {
		case '\r', '\n':
			return string(line), nil
		case 3, 4: // Ctrl-C, Ctrl-D
			return "", errors.New(i18n.T("已取消", "Cancelled"))
		case 8, 127:
			if len(line) > 0 {
				line = line[:len(line)-1]
				for len(line) > 0 && line[len(line)-1]&0xC0 == 0x80 { // the rest of a character
					line = line[:len(line)-1]
				}
			}
		default:
			if b >= 32 {
				line = append(line, b)
			}
		}
	}
}

func runConfigExport(c *Ctx) error {
	path, err := filepath.Abs(c.arg(0))
	if err != nil {
		return err
	}
	password := c.env.Getenv(passwordEnv)
	if password == "" && c.has("password") {
		if password, err = c.askPassword(i18n.T("密码：", "Password: ")); err != nil {
			return err
		}
		again, err := c.askPassword(i18n.T("再输入一次：", "Once more: "))
		if err != nil {
			return err
		}
		if password == "" || again != password {
			return errors.New(i18n.T("两次输入的密码不一样，或者是空的", "The two passwords differ, or are empty"))
		}
	}
	raw, err := c.call("config_export", map[string]any{"path": path, "password": password})
	if err != nil {
		return err
	}
	c.done(raw, fmt.Sprintf(i18n.T("已导出到 %s", "Exported to %s"), path))
	if password == "" {
		c.warn(i18n.T("文件没有加密，里面有订阅链接和节点信息；要加密请加 --password", "The file is not encrypted and holds your subscription links and nodes; add --password to encrypt it"))
	}
	return nil
}

func runConfigImport(c *Ctx) error {
	undo := c.has("undo")
	if undo == (c.arg(0) != "") {
		return errors.New(i18n.T("请给出要导入的文件，或者用 --undo 撤销上次导入", "Name the file to import, or use --undo to undo the last import"))
	}
	args := map[string]any{"undo": undo}
	if !undo {
		path, err := filepath.Abs(c.arg(0))
		if err != nil {
			return err
		}
		args["path"], args["password"] = path, c.env.Getenv(passwordEnv)
	}
	type answer struct {
		Imported, NeedPassword bool
		Message                string
	}
	var res answer
	raw, err := c.get("config_import", args, &res)
	if err != nil {
		return err
	}
	if res.NeedPassword && args["password"] == "" {
		pw, err := c.askPassword(i18n.T("这个文件设了密码：", "This file's password: "))
		if err != nil {
			return err
		}
		args["password"] = pw
		res = answer{}
		if raw, err = c.get("config_import", args, &res); err != nil {
			return err
		}
	}
	if res.NeedPassword {
		return errors.New(res.Message)
	}
	if undo {
		c.done(raw, i18n.T("已撤销上次导入", "Undid the last import"))
		return nil
	}
	c.done(raw, i18n.T("已导入；本机的端口、局域网和接管设置没有变。要撤销：bowel config import --undo", "Imported; this machine's ports, LAN and takeover settings are as they were. To undo: bowel config import --undo"))
	return nil
}
