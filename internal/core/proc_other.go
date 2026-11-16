//go:build !unix

package core

import (
	"os/exec"
	"time"
)

func configureCheckProcess(cmd *exec.Cmd) {
	cmd.WaitDelay = 5 * time.Second
}
