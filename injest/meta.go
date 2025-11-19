package injest

import (
	"fmt"
	"runtime/debug"
)

var (
	userAgent string
	// The version of the currently running executable
	version string
)

func init() {
	buildInfo, ok := debug.ReadBuildInfo()
	if !ok {
		panic("Build info not available")
	}
	userAgent = fmt.Sprintf("VideoListingBot/%s (https://%s)", buildInfo.Main.Version, buildInfo.Main.Path)
	fmt.Printf("%+v\n", buildInfo)
	for _, setting := range buildInfo.Settings {
		if setting.Key == "vcs.revision" {
			version = setting.Value
			break
		}
	}
}
