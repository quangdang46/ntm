//go:build windows

package alerts

import (
	"os"

	"golang.org/x/sys/windows"
)

// checkDiskSpace verifies available disk space (Windows implementation).
//
// Uses GetDiskFreeSpaceExW so the reported value reflects the free bytes
// available to the calling user (i.e. honors per-user disk quotas). Falls
// back to %SystemDrive% (typically "C:\") when the configured ProjectsDir
// can't be queried — mirroring the Unix fallback to "/".
func (g *Generator) checkDiskSpace() (*Alert, error) {
	checkPath := defaultWindowsCheckPath()
	if g.config.ProjectsDir != "" {
		checkPath = g.config.ProjectsDir
	}

	freeBytes, err := windowsFreeBytesAvailable(checkPath)
	if err != nil {
		fallback := defaultWindowsCheckPath()
		if checkPath != fallback {
			checkPath = fallback
			freeBytes, err = windowsFreeBytesAvailable(checkPath)
		}
		if err != nil {
			return nil, err
		}
	}

	freeGB := float64(freeBytes) / (1024 * 1024 * 1024)
	return g.buildDiskSpaceAlert(freeGB, checkPath), nil
}

// windowsFreeBytesAvailable wraps GetDiskFreeSpaceExW for a single path.
// The reported value is the number of free bytes available to the calling
// user, which already accounts for per-user disk quotas.
func windowsFreeBytesAvailable(path string) (uint64, error) {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}

	var freeBytesAvailableToCaller, totalBytes, totalFreeBytes uint64
	if err := windows.GetDiskFreeSpaceEx(pathPtr, &freeBytesAvailableToCaller, &totalBytes, &totalFreeBytes); err != nil {
		return 0, err
	}
	return freeBytesAvailableToCaller, nil
}

// defaultWindowsCheckPath returns the system drive (typically "C:\")
// or "C:\" if SystemDrive isn't set in the environment.
func defaultWindowsCheckPath() string {
	if drive := os.Getenv("SystemDrive"); drive != "" {
		return drive + `\`
	}
	return `C:\`
}
