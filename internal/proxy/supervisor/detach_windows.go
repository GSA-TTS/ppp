//go:build windows

package supervisor

import "syscall"

// detachSysProcAttr on Windows returns nil; process detachment differs and the
// v1 target is macOS/Linux. Present so the package compiles cross-platform.
func detachSysProcAttr() *syscall.SysProcAttr { return nil }
