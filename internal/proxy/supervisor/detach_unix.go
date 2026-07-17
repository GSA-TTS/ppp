//go:build !windows

package supervisor

import "syscall"

// detachSysProcAttr returns SysProcAttr that puts the spawned mitmdump in its
// own session (Setsid), so the daemon is not killed by SIGHUP when the
// launching `ppp` process's controlling terminal goes away. This is what lets
// the proxy "start once and stay running" (spec §5.8/§6.15).
func detachSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
