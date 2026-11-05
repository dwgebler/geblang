package main

import (
	"fmt"
	"os"
	"strings"
)

// topLevelCommands is the canonical set of geblang subcommands and the
// single source for shell completion. A guard test keeps it in sync
// with the documented command set so completion cannot lag the CLI.
var topLevelCommands = []string{
	"run", "build", "install", "resolve", "fmt", "check", "test", "doc", "bind",
	"lsp", "dap", "doctor", "cache", "init", "completion", "licenses",
	"help", "version",
}

func runCompletion(args []string) {
	shell := ""
	if len(args) > 0 {
		shell = args[0]
	}
	switch shell {
	case "bash":
		fmt.Fprint(os.Stdout, bashCompletionScript())
	default:
		fmt.Fprintln(os.Stderr, "usage: geblang completion bash")
		fmt.Fprintln(os.Stderr, "Emit a shell completion script. Enable it with, e.g.:")
		fmt.Fprintln(os.Stderr, "  source <(geblang completion bash)")
		if shell != "" && shell != "-h" && shell != "--help" {
			fmt.Fprintf(os.Stderr, "unsupported shell %q (supported: bash)\n", shell)
		}
		os.Exit(2)
	}
}

func bashCompletionScript() string {
	return fmt.Sprintf(`# bash completion for geblang. Enable with:
#   source <(geblang completion bash)
_geblang_complete() {
    local cur
    cur="${COMP_WORDS[COMP_CWORD]}"
    if [ "${COMP_CWORD}" -eq 1 ]; then
        COMPREPLY=( $(compgen -W %q -- "${cur}") )
    else
        COMPREPLY=( $(compgen -f -- "${cur}") )
    fi
}
complete -o filenames -F _geblang_complete geblang
`, strings.Join(topLevelCommands, " "))
}
