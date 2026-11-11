package main

// The commands' wiring: internal/cli parses, talks to the running bowel
// and prints; what must run in this process (the daemon, the MCP bridge,
// undoing the developer tools) is handed to it from here.

import (
	"fmt"
	"os"

	"github.com/egoist/mygo"

	"github.com/oboo/moho/internal/cli"
	"github.com/oboo/moho/internal/i18n"
)

func runCLI(args []string) int {
	attachConsole()
	args, dir := cli.DataDir(args)
	if dir != "" {
		if err := useDataDir(dir); err != nil {
			fmt.Fprintln(os.Stderr, "bowel:", err)
			return 1
		}
	}
	data, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		fmt.Fprintln(os.Stderr, "bowel:", err)
		return 1
	}
	i18n.Set(cliLanguage(data))
	return cli.Run(cli.Env{
		Args: args, Stdout: os.Stdout, Stderr: os.Stderr, TTY: cli.IsTerminal(os.Stdout),
		Getenv: os.Getenv, Socket: cliSocket(data), Version: cliVersion(),
		Local: map[string]func([]string) error{
			"daemon":   runDaemon,
			"mcp":      func([]string) error { return runMCPBridge() },
			"devtools": runDevtoolsCLI,
		},
	})
}
