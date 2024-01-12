package multipass

import (
	"os"
	"path"
)

const library_folder = "Library/DesktopAutoscalerUtility"

func desktopUtilityTempDirectory() string {
	if home, err := os.UserHomeDir(); err != nil {
		return ""
	} else {
		return path.Join(home, library_folder)
	}
}
