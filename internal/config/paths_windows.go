package config

import "os"

// defaultOneFileSystem : sous Windows, les dossiers utilisateur peuvent
// s'étendre sur plusieurs lecteurs et l'option écarterait silencieusement des
// données.
const defaultOneFileSystem = false

// stateRoot pointe sur %LOCALAPPDATA% : données locales à la machine, non
// répliquées par les profils itinérants, contrairement à %APPDATA%.
func stateRoot() (string, error) {
	return os.UserCacheDir()
}
