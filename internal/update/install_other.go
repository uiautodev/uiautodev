//go:build !darwin && !windows

package update

import (
	"fmt"
	"runtime"
)

func installDesktopAsset(_, _ string) error {
	return fmt.Errorf("desktop updates are not available on %s", runtime.GOOS)
}
