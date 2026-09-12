//go:build !windows

package agent

import "os"

func protectStateDirectory(path string) error {
	return os.Chmod(path, 0o700)
}
