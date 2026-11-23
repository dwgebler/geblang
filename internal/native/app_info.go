package native

import "sync/atomic"

type AppPackage struct {
	Name    string
	Version string
}

type AppInfo struct {
	Name     string
	Version  string
	Packages []AppPackage
}

var bundledAppInfo atomic.Pointer[AppInfo]

// SetBundledAppInfo records the built binary's manifest identity for sys.appInfo.
func SetBundledAppInfo(info AppInfo) { bundledAppInfo.Store(&info) }

func BundledAppInfo() (AppInfo, bool) {
	info := bundledAppInfo.Load()
	if info == nil {
		return AppInfo{}, false
	}
	return *info, true
}
