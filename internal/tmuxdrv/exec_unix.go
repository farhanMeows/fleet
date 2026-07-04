//go:build unix

package tmuxdrv

import (
	"os"
	"syscall"
)

// syscallExec replaces the current process — used for `fleet up` attaching
// to tmux so the terminal is handed over cleanly.
func syscallExec(path string, argv []string) error {
	return syscall.Exec(path, argv, os.Environ())
}
