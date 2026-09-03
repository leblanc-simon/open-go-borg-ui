package borg

import "testing"

// TestToDriveRelative couvre la convention d'écriture des archives Windows :
// la lettre de lecteur est la première composante, sans préfixe cygdrive.
func TestToDriveRelative(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"chemin simple", `C:\Users\marc\Documents`, "c/Users/marc/Documents"},
		{"autre lecteur", `D:\Projets`, "d/Projets"},
		{"lecteur en minuscule", `d:\Projets`, "d/Projets"},
		{"racine du lecteur", `C:\`, "c"},
		{"barre finale", `C:\Users\marc\`, "c/Users/marc"},
		{"séparateurs redondants", `C:\\Users\\marc`, "c/Users/marc"},
		{"espaces et accents", `C:\Users\marc\Mes Documents\Été 2026`, "c/Users/marc/Mes Documents/Été 2026"},
		{"chemin long", `\\?\C:\Users\marc\Documents`, "c/Users/marc/Documents"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := toDriveRelative(tc.in)
			if err != nil {
				t.Fatalf("toDriveRelative(%q) a échoué: %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("toDriveRelative(%q) = %q, attendu %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestToDriveRelativeRejette vérifie que les chemins hors convention sont
// refusés plutôt que traduits approximativement : une archive écrite avec des
// chemins inattendus serait très coûteuse à corriger après coup.
func TestToDriveRelativeRejette(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"chemin réseau", `\\serveur\partage\dossier`},
		{"chemin relatif", `Documents\rapport.odt`},
		{"chemin POSIX", "/home/marc/Documents"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := toDriveRelative(tc.in); err == nil {
				t.Errorf("toDriveRelative(%q) = %q, une erreur était attendue", tc.in, got)
			}
		})
	}
}

// TestToCygwinPath couvre les chemins de service, que Borg ouvre lui-même.
func TestToCygwinPath(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{`C:\Users\marc\.config\borgui\id_ed25519`, "/cygdrive/c/Users/marc/.config/borgui/id_ed25519"},
		{`E:\`, "/cygdrive/e"},
		{"/déjà/posix", "/déjà/posix"},
	}

	for _, tc := range cases {
		if got := toCygwinPath(tc.in); got != tc.want {
			t.Errorf("toCygwinPath(%q) = %q, attendu %q", tc.in, got, tc.want)
		}
	}
}

// TestDriveRoot vérifie la racine utilisée pour une restauration à
// l'emplacement d'origine.
func TestDriveRoot(t *testing.T) {
	drive, err := driveLetter(`c:\Users\marc`)
	if err != nil {
		t.Fatalf("driveLetter a échoué: %v", err)
	}
	if got, want := driveRoot(drive), `C:\`; got != want {
		t.Errorf("driveRoot = %q, attendu %q", got, want)
	}
}
