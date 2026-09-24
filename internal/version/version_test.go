package version

import (
	"runtime/debug"
	"testing"
)

func info(settings ...debug.BuildSetting) func() (*debug.BuildInfo, bool) {
	return func() (*debug.BuildInfo, bool) { return &debug.BuildInfo{Settings: settings}, true }
}

func TestResolve(t *testing.T) {
	revision := debug.BuildSetting{Key: "vcs.revision", Value: "809de26a1b2c3d4e5f60718293a4b5c6d7e8f901"}
	cases := []struct {
		name     string
		injected string
		info     func() (*debug.BuildInfo, bool)
		want     string
	}{
		{"tag injecté", "v1.0.0", info(revision), "v1.0.0"},
		{"commit", "", info(revision, debug.BuildSetting{Key: "vcs.modified", Value: "false"}), "809de26a1b2c"},
		{"commit modifié", "", info(revision, debug.BuildSetting{Key: "vcs.modified", Value: "true"}), "809de26a1b2c-dirty"},
		{"hors dépôt", "", info(), "dev"},
		{"sans informations", "", func() (*debug.BuildInfo, bool) { return nil, false }, "dev"},
	}
	for _, c := range cases {
		if got := resolve(c.injected, c.info); got != c.want {
			t.Errorf("%s: %q, attendu %q", c.name, got, c.want)
		}
	}
}
