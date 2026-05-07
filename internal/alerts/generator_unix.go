//go:build unix

package alerts

import (
	"syscall"
)

// checkDiskSpace verifies available disk space (Unix implementation)
func (g *Generator) checkDiskSpace() (*Alert, error) {
	var stat syscall.Statfs_t

	// Check space on project directory if configured, otherwise root
	checkPath := "/"
	if g.config.ProjectsDir != "" {
		checkPath = g.config.ProjectsDir
	}

	err := syscall.Statfs(checkPath, &stat)
	if err != nil {
		// If project dir check fails (e.g. doesn't exist), fallback to root
		if checkPath != "/" {
			checkPath = "/"
			err = syscall.Statfs(checkPath, &stat)
		}
		if err != nil {
			return nil, err
		}
	}

	// Calculate free space in GB.
	// Convert to float64 directly to handle different field types across Unix variants
	// (Bavail is int64 on Linux/FreeBSD, uint64 on macOS).
	freeGB := float64(stat.Bavail) * float64(stat.Bsize) / (1024 * 1024 * 1024)

	return g.buildDiskSpaceAlert(freeGB, checkPath), nil
}
