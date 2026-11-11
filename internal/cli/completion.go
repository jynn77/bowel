package cli

import (
	"fmt"
	"sort"
	"strings"
)

// completions maps each command, as the words that lead to it (" rules
// add"; "" is bowel itself), to what may come next: its subcommands, the
// choices of its first argument, and its options.
func completions(root *Command) map[string][]string {
	out := map[string][]string{}
	var walk func(cmd *Command, path string)
	walk = func(cmd *Command, path string) {
		var w []string
		for _, s := range cmd.Sub {
			w = append(w, s.Name)
		}
		w = append(w, cmd.Choices...)
		if !cmd.Raw {
			for _, f := range cmd.Flags {
				w = append(w, "--"+f.Name)
			}
			for _, f := range globalFlags {
				w = append(w, "--"+f.Name)
			}
		}
		out[path] = w
		for _, s := range cmd.Sub {
			walk(s, path+" "+s.Name)
		}
	}
	walk(root, "")
	return out
}

func runCompletion(c *Ctx) error {
	table := completions(c.root)
	paths := make([]string, 0, len(table))
	for p := range table {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var b strings.Builder
	switch c.args[0] {
	case "bash":
		b.WriteString(`# bash completion for bowel: source it, or put it in /etc/bash_completion.d/
_bowel() {
  local cur="${COMP_WORDS[COMP_CWORD]}" path="" i
  for ((i = 1; i < COMP_CWORD; i++)); do
    case "${COMP_WORDS[i]}" in -*) ;; *) path="$path ${COMP_WORDS[i]}" ;; esac
  done
  local words=""
  case "$path" in
`)
		for _, p := range paths {
			fmt.Fprintf(&b, "    %q) words=%q ;;\n", p, strings.Join(table[p], " "))
		}
		b.WriteString(`  esac
  COMPREPLY=($(compgen -W "$words" -- "$cur"))
}
complete -o default -F _bowel bowel
`)
	case "zsh":
		b.WriteString(`#compdef bowel
# zsh completion for bowel: save it as _bowel in a directory of $fpath.
_bowel() {
  local cmd="" i
  for ((i = 2; i < CURRENT; i++)); do
    case "${words[i]}" in -*) ;; *) cmd="$cmd ${words[i]}" ;; esac
  done
  local -a next
  case "$cmd" in
`)
		for _, p := range paths {
			fmt.Fprintf(&b, "    %q) next=(%s) ;;\n", p, strings.Join(table[p], " "))
		}
		b.WriteString(`  esac
  if (( ${#next} )); then compadd -a next; else _files; fi
}
if [ "$funcstack[1]" = "_bowel" ]; then _bowel "$@"; else compdef _bowel bowel; fi
`)
	case "fish":
		b.WriteString(`# fish completion for bowel: save it as ~/.config/fish/completions/bowel.fish
function __bowel_path
    set -l path ""
    for w in (commandline -opc)[2..-1]
        string match -q -- '-*' $w; or set path "$path $w"
    end
    echo -- "$path"
end
complete -c bowel -f
`)
		for _, p := range paths {
			if len(table[p]) == 0 {
				continue
			}
			fmt.Fprintf(&b, "complete -c bowel -n 'test (__bowel_path) = %q' -a %q\n", p, strings.Join(table[p], " "))
		}
	case "powershell":
		b.WriteString(`# PowerShell completion for bowel: add it to $PROFILE.
$__bowelWords = @{
`)
		for _, p := range paths {
			fmt.Fprintf(&b, "  '%s' = '%s'\n", p, strings.Join(table[p], " "))
		}
		b.WriteString(`}
Register-ArgumentCompleter -Native -CommandName bowel -ScriptBlock {
  param($wordToComplete, $commandAst, $cursorPosition)
  $path = ''
  $elements = $commandAst.CommandElements | Select-Object -Skip 1 | ForEach-Object { $_.ToString() }
  foreach ($e in $elements) {
    if ($e -eq $wordToComplete) { continue }
    if (-not $e.StartsWith('-')) { $path += " $e" }
  }
  if ($__bowelWords.ContainsKey($path)) {
    $__bowelWords[$path] -split ' ' | Where-Object { $_ -like "$wordToComplete*" } |
      ForEach-Object { [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_) }
  }
}
`)
	}
	fmt.Fprint(c.out, b.String())
	return nil
}
