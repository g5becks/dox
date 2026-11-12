package main

import (
	"runtime/debug"
	"testing"
)

func TestResolveBuildMetadata(t *testing.T) {
	t.Parallel()
	unstamped := buildMetadata{version: devVersion, commit: unknownMetadata, built: unknownMetadata}
	release := buildMetadata{version: "0.5.0", commit: "abc1234", built: "2026-01-02T03:04:05Z"}
	stamped := &debug.BuildInfo{
		Main: debug.Module{Version: "v0.5.0"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "0123456789abcdef"},
			{Key: "vcs.time", Value: "2026-03-04T05:06:07Z"},
			{Key: "vcs.modified", Value: "false"},
		},
	}
	tests := []struct {
		name     string
		injected buildMetadata
		info     *debug.BuildInfo
		want     buildMetadata
	}{
		{"no build info", unstamped, nil, unstamped},
		{
			"go install at a version",
			unstamped,
			stamped,
			buildMetadata{version: "v0.5.0", commit: "0123456789abcdef", built: "2026-03-04T05:06:07Z"},
		},
		{
			"ldflags win over build info",
			release,
			stamped,
			release,
		},
		{
			"devel main module keeps dev",
			unstamped,
			&debug.BuildInfo{Main: debug.Module{Version: "(devel)"}},
			unstamped,
		},
		{
			"dirty checkout",
			unstamped,
			&debug.BuildInfo{
				Main: debug.Module{Version: "v0.4.1-0.20260101000000-0123456789ab+dirty"},
				Settings: []debug.BuildSetting{
					{Key: "vcs.revision", Value: "0123456789abcdef"},
					{Key: "vcs.modified", Value: "true"},
				},
			},
			buildMetadata{
				version: "v0.4.1-0.20260101000000-0123456789ab+dirty",
				commit:  "0123456789abcdef-dirty",
				built:   unknownMetadata,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := resolveBuildMetadata(tt.injected, tt.info); got != tt.want {
				t.Errorf("resolveBuildMetadata() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
