package util

import (
	"fmt"
	"runtime/debug"
)

func UserAgent() string {
	version := "dev"
	if info, ok := debug.ReadBuildInfo(); ok &&
		info.Main.Version != "" && info.Main.Version != "(devel)" {
		version = info.Main.Version
	}
	return fmt.Sprintf("charge/%s", version)
}
