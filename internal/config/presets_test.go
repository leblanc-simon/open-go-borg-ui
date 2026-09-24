package config

import (
	"slices"
	"strings"
	"testing"
)

// TestPreselections vérifie qu'un préréglage s'ajoute et se retire sans
// toucher aux motifs saisis par l'utilisateur, et que ses motifs restent
// portables : aucun chemin absolu (EF-43).
func TestPreselections(t *testing.T) {
	preset := ExcludePresets[2] // node_modules
	excludes := []string{"**/*.bak"}

	excludes = SetPreset(excludes, preset, true)
	if !PresetEnabled(excludes, preset) || !slices.Contains(excludes, "**/*.bak") {
		t.Errorf("après activation: %v", excludes)
	}
	excludes = SetPreset(excludes, preset, true)
	if strings.Count(strings.Join(excludes, " "), "**/node_modules") != 1 {
		t.Errorf("activation répétée: %v", excludes)
	}
	excludes = SetPreset(excludes, preset, false)
	if PresetEnabled(excludes, preset) || !slices.Equal(excludes, []string{"**/*.bak"}) {
		t.Errorf("après retrait: %v", excludes)
	}

	for _, p := range ExcludePresets {
		for _, pattern := range p.Patterns {
			if strings.HasPrefix(pattern, "/") || strings.Contains(pattern, ":\\") {
				t.Errorf("%s: motif non portable %q", p.Key, pattern)
			}
		}
	}
}
