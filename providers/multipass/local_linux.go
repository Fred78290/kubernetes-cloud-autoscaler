package multipass

import (
	"os"
	"path"
)

func desktopUtilityTempDirectory() string {
	if home, err := os.UserHomeDir(); err != nil {
		return ""
	} else {
		return path.Join(home, "tmp/autoscaler-utility")
	}
}
