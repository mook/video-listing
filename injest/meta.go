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
	fmt.Printf("%+v\n", buildInfo)
	if version == "" {
		version = buildInfo.Main.Version
	}
	if version == "" || version == "(devel)" {
		for _, setting := range buildInfo.Settings {
			if setting.Key == "vcs.revision" {
				version = setting.Value
				break
			}
		}
	}
	userAgent = fmt.Sprintf("VideoListingBot/%s (https://%s)", version, buildInfo.Main.Path)
}
