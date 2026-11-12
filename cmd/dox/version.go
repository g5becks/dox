package main

import (
	"fmt"
	"runtime/debug"
)

const (
	devVersion      = "dev"
	unknownMetadata = "unknown"
	develBuildInfo  = "(devel)"
)

type buildMetadata struct {
	version string
	commit  string
	built   string
}

func versionString() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		info = nil
	}
	meta := resolveBuildMetadata(buildMetadata{version: version, commit: commit, built: buildTime}, info)
	return fmt.Sprintf("%s (commit %s, built %s)", meta.version, meta.commit, meta.built)
}

// resolveBuildMetadata prefers the values injected with -ldflags (releases)
// and falls back to the build information the Go toolchain embeds, so that
// `go install` and `go build` in a checkout report a real version, commit and
// time instead of "dev (commit unknown, built unknown)".
func resolveBuildMetadata(injected buildMetadata, info *debug.BuildInfo) buildMetadata {
	meta := injected
	if info == nil {
		return meta
	}
	if meta.version == devVersion && info.Main.Version != "" && info.Main.Version != develBuildInfo {
		meta.version = info.Main.Version
	}
	settings := make(map[string]string, len(info.Settings))
	for _, setting := range info.Settings {
		settings[setting.Key] = setting.Value
	}
	if meta.commit == unknownMetadata && settings["vcs.revision"] != "" {
		meta.commit = settings["vcs.revision"]
		if settings["vcs.modified"] == "true" {
			meta.commit += "-dirty"
		}
	}
	if meta.built == unknownMetadata && settings["vcs.time"] != "" {
		meta.built = settings["vcs.time"]
	}
	return meta
}
