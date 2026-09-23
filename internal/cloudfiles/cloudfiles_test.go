package cloudfiles

import "testing"

// TestAttributs fixe la lecture des attributs relevés sur des fichiers
// OneDrive : seul un contenu absent du disque désigne un fichier à écarter.
func TestAttributs(t *testing.T) {
	cases := []struct {
		name       string
		attributes uint32
		want       bool
	}{
		{"fichier ordinaire", 0x20, false},                                     // ARCHIVE
		{"disponible en ligne uniquement", 0x00400000 | 0x400 | 0x20, true},    // + REPARSE_POINT
		{"toujours conservé sur l'appareil", 0x00080000 | 0x400 | 0x20, false}, // PINNED
		{"hors connexion (ancienne forme)", 0x1000, true},
		{"en lecture seule", 0x1, false},
	}
	for _, c := range cases {
		if got := isPlaceholder(c.attributes); got != c.want {
			t.Errorf("%s (%#x): %v, attendu %v", c.name, c.attributes, got, c.want)
		}
	}
}
