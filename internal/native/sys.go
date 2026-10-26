package native

import (
	"os"
	"os/user"
	"runtime"
	"runtime/debug"
)

func sysHostname() (string, error) { return os.Hostname() }
func sysPid() int                  { return os.Getpid() }
func sysPlatform() string          { return runtime.GOOS }
func sysArch() string              { return runtime.GOARCH }
func sysCPUCount() int             { return runtime.GOMAXPROCS(0) }
func sysSetGCPercent(p int) int    { return debug.SetGCPercent(p) }
func sysTmpDir() string            { return os.TempDir() }
func sysEnviron() []string         { return os.Environ() }

func sysHomeDir() (string, error) {
	u, err := user.Current()
	if err != nil {
		return "", err
	}
	return u.HomeDir, nil
}

func sysUsername() (string, error) {
	u, err := user.Current()
	if err != nil {
		return "", err
	}
	return u.Username, nil
}
