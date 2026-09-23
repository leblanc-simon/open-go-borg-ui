package config

import "testing"

// TestNomSur vérifie qu'un nom de profil quelconque donne un nom de fichier
// sûr, et que deux noms distincts le restent.
func TestNomSur(t *testing.T) {
	cases := map[string]string{
		"poste":         "poste",
		"Poste de Marc": "Poste_20de_20Marc",
		"été/../x":      "_e9t_e9_2f_2e_2e_2fx",
		"":              "default",
	}
	for in, want := range cases {
		if got := SafeName(in); got != want {
			t.Errorf("SafeName(%q) = %q, attendu %q", in, got, want)
		}
	}
}
