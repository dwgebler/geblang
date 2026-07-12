// Package wslpath converts Windows-side WSL paths (\\wsl.localhost\<distro>\..., C:\...) to their in-WSL POSIX form.
package wslpath

import (
	"strings"
	"unicode"
)

// UNCToLinux converts a \\wsl.localhost\<distro>\... or \\wsl$\... UNC path to the in-WSL path.
func UNCToLinux(path string) (string, bool) {
	normalized := strings.ReplaceAll(path, "/", "\\")
	for _, prefix := range []string{`\\wsl.localhost\`, `\\wsl$\`, `\\wsl\`} {
		if !strings.HasPrefix(strings.ToLower(normalized), strings.ToLower(prefix)) {
			continue
		}
		rest := normalized[len(prefix):]
		parts := strings.SplitN(rest, `\`, 2)
		if len(parts) != 2 || parts[1] == "" {
			return "", false
		}
		return "/" + strings.ReplaceAll(parts[1], `\`, "/"), true
	}
	return "", false
}

// DriveToWSL converts a Windows drive path (C:\Users\...) to its /mnt/<drive> WSL form.
func DriveToWSL(path string) (string, bool) {
	if !LooksLikeWindowsDrive(path) {
		return "", false
	}
	drive := unicode.ToLower(rune(path[0]))
	rest := path[2:]
	rest = strings.TrimLeft(rest, `\/`)
	rest = strings.ReplaceAll(rest, `\`, "/")
	if rest == "" {
		return "/mnt/" + string(drive), true
	}
	return "/mnt/" + string(drive) + "/" + rest, true
}

// LooksLikeWindowsDrive reports whether path starts with a drive letter and colon.
func LooksLikeWindowsDrive(path string) bool {
	return len(path) >= 2 && unicode.IsLetter(rune(path[0])) && path[1] == ':'
}
