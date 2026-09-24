// Package version donne la version de l'exécutable.
//
// Une version publiée porte son tag (v1.0.0), injecté à la compilation par le
// Makefile :
//
//	go build -ldflags "-X leblanc.io/open-go-borg-ui/internal/version.Version=v1.0.0"
//
// Sans lui, en développement, la version est l'identifiant du commit, que Go
// enregistre de lui-même dans tout exécutable compilé depuis un dépôt git.
// Des modifications non commitées le marquent de « -dirty », comme le fait
// git describe.
package version

import (
	"runtime/debug"
	"sync"
)

// Version est la version injectée à la compilation. Vide, elle se déduit
// des informations de compilation.
var Version = ""

// commitLength est la longueur de l'identifiant de commit affiché, celle de
// git rev-parse --short=12.
const commitLength = 12

var (
	once     sync.Once
	resolved string
)

// String retourne la version de l'exécutable : son tag, l'identifiant du
// commit dont il est issu, ou « dev » faute de l'un et de l'autre.
func String() string {
	once.Do(func() { resolved = resolve(Version, debug.ReadBuildInfo) })
	return resolved
}

// resolve choisit la version : celle injectée, sinon le commit enregistré
// par Go.
func resolve(injected string, buildInfo func() (*debug.BuildInfo, bool)) string {
	if injected != "" {
		return injected
	}
	info, ok := buildInfo()
	if !ok {
		return "dev"
	}
	var revision string
	var dirty bool
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			dirty = setting.Value == "true"
		}
	}
	if revision == "" {
		return "dev"
	}
	if len(revision) > commitLength {
		revision = revision[:commitLength]
	}
	if dirty {
		revision += "-dirty"
	}
	return revision
}
