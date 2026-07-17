package cli

import "syscall"

// unixSignal0 returns signal 0, used to probe process liveness without
// affecting the target (kill -0). Isolated here so supervisor.go stays free of
// syscall imports and platform assumptions are in one place.
func unixSignal0() syscall.Signal { return syscall.Signal(0) }
