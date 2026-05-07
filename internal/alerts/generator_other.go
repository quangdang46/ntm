//go:build !unix && !windows

package alerts

// checkDiskSpace on genuinely unsupported platforms (Plan 9, JS, wasip1, etc.)
// returns (nil, nil) so the alert generator treats disk-space as healthy
// rather than failed. Linux, macOS, *BSD use generator_unix.go and Windows
// uses generator_windows.go.
//
// If a new GOOS needs disk-space alerts, add a platform-specific
// implementation alongside the existing two and tighten the build tag here.
func (g *Generator) checkDiskSpace() (*Alert, error) {
	return nil, nil
}
