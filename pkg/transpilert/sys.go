package transpilert

import (
	"os"
	"os/user"
	"runtime"
	"strings"
)

// Typed adapters for the Geblang sys module over the os/runtime packages.
// getenv returns null (nil) for an unset variable, matching the interpreter.

// SysPlatform returns the OS name (runtime.GOOS).
func SysPlatform() string { return runtime.GOOS }

// SysArch returns the architecture (runtime.GOARCH).
func SysArch() string { return runtime.GOARCH }

// SysCPUCount returns how many CPUs the process can run code on at once
// (runtime.GOMAXPROCS).
func SysCPUCount() int64 { return int64(runtime.GOMAXPROCS(0)) }

// SysGetenv returns the value of name, or nil when the variable is unset.
func SysGetenv(name string) any {
	if v, ok := os.LookupEnv(name); ok {
		return v
	}
	return nil
}

// SysSetenv sets an environment variable.
func SysSetenv(name, value string) any {
	if err := os.Setenv(name, value); err != nil {
		panic(&Error{Class: "RuntimeError", Message: err.Error(), Parents: []string{"Error"}})
	}
	return nil
}

// SysCwd returns the current working directory.
func SysCwd() string {
	dir, err := os.Getwd()
	if err != nil {
		panic(&Error{Class: "RuntimeError", Message: err.Error(), Parents: []string{"Error"}})
	}
	return dir
}

// SysHostname returns the host name.
func SysHostname() string {
	name, err := os.Hostname()
	if err != nil {
		panic(&Error{Class: "RuntimeError", Message: "sys.hostname: " + err.Error(), Parents: []string{"Error"}})
	}
	return name
}

// SysUsername returns the current user's username.
func SysUsername() string {
	u, err := user.Current()
	if err != nil {
		panic(&Error{Class: "RuntimeError", Message: "sys.username: " + err.Error(), Parents: []string{"Error"}})
	}
	return u.Username
}

// SysEnviron returns all environment variables as a name->value dict.
func SysEnviron() *OrderedDict[string, string] {
	d := NewOrderedDict[string, string]()
	for _, kv := range os.Environ() {
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			d.Set(kv, "")
			continue
		}
		d.Set(kv[:eq], kv[eq+1:])
	}
	return d
}
