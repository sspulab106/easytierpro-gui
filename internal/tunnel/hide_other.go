//go:build !windows

package tunnel

import "os/exec"

func hide(_ *exec.Cmd) {}

// assignJob is a no-op outside Windows (Unix children already die with the
// process group / explicit StopAll on shutdown).
func assignJob(_ *exec.Cmd) {}
