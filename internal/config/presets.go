package config

// ExcludePreset est un préréglage d'exclusion proposé en case à cocher
// (EF-42). Ses motifs sont portables : ils valent sous Windows comme sous
// Linux (EF-43).
type ExcludePreset struct {
	// Key identifie le préréglage et nomme son libellé,
	// « exclude.<Key> » au catalogue.
	Key      string
	Patterns []string
}

// ExcludePresets sont les préréglages proposés, dans l'ordre d'affichage.
var ExcludePresets = []ExcludePreset{
	{Key: "temporary", Patterns: []string{"**/*.tmp", "**/~$*", "**/.~lock.*#"}},
	{Key: "browser_caches", Patterns: []string{
		"**/Google/Chrome/*/*Cache*",
		"**/Microsoft/Edge/*/*Cache*",
		"**/Mozilla/Firefox/Profiles/*/cache2",
		"**/.cache/mozilla",
		"**/.cache/google-chrome",
		"**/.cache/chromium",
	}},
	{Key: "node_modules", Patterns: []string{"**/node_modules"}},
	{Key: "disk_images", Patterns: []string{"**/*.iso", "**/*.vmdk", "**/*.vdi", "**/*.qcow2"}},
	{Key: "trash", Patterns: []string{"**/$RECYCLE.BIN", "**/.Trash-*", "**/.local/share/Trash"}},
}

// PresetEnabled indique si tous les motifs du préréglage figurent dans
// excludes.
func PresetEnabled(excludes []string, preset ExcludePreset) bool {
	present := make(map[string]bool, len(excludes))
	for _, pattern := range excludes {
		present[pattern] = true
	}
	for _, pattern := range preset.Patterns {
		if !present[pattern] {
			return false
		}
	}
	return true
}

// SetPreset ajoute ou retire les motifs d'un préréglage, sans toucher aux
// autres motifs, ni aux motifs libres saisis par l'utilisateur.
func SetPreset(excludes []string, preset ExcludePreset, enabled bool) []string {
	own := make(map[string]bool, len(preset.Patterns))
	for _, pattern := range preset.Patterns {
		own[pattern] = true
	}
	result := make([]string, 0, len(excludes)+len(preset.Patterns))
	for _, pattern := range excludes {
		if !own[pattern] {
			result = append(result, pattern)
		}
	}
	if enabled {
		result = append(result, preset.Patterns...)
	}
	return result
}
