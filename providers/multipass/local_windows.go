package multipass

func expandPath(ePath string) string {
	expandedPath := strings.ToLower(ePath)
	systemDrive := os.Getenv("SystemRoot")[0:2]
	expandedPath = strings.Replace(expandedPath, "%homedrive%", os.Getenv("HOMEDRIVE"), -1)
	expandedPath = strings.Replace(expandedPath, "%systemroot%", os.Getenv("SystemRoot"), -1)
	expandedPath = strings.Replace(expandedPath, "%systemdrive%", systemDrive, -1)
	return expandedPath
}

func desktopUtilityTempDirectory() string {
	return expandPath(filepath.Join(os.UserHomeDir(), "AppData"
		"AlduneLabs", "kubernetes-desktop-autoscaler-utility"))
}
