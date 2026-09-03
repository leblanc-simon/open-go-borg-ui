package borg

import (
	"strings"
	"testing"
)

// lookup retrouve la dernière valeur d'une variable dans un environnement.
func lookup(env []string, key string) (string, bool) {
	value, found := "", false
	for _, entry := range env {
		if rest, ok := strings.CutPrefix(entry, key+"="); ok {
			value, found = rest, true
		}
	}
	return value, found
}

// TestEnvironnementNonChiffre couvre le piège le plus coûteux du mode non
// chiffré : sans la variable d'acceptation, Borg pose une question interactive
// au premier accès et la sauvegarde planifiée reste bloquée indéfiniment.
func TestEnvironnementNonChiffre(t *testing.T) {
	env := Environment{
		Repository:     "ssh://u1@u1.your-storagebox.de:23/./poste",
		Encrypted:      false,
		PassCommandExe: "/usr/local/bin/borgui",
	}.environ(nativePath)

	if value, ok := lookup(env, "BORG_UNKNOWN_UNENCRYPTED_REPO_ACCESS_IS_OK"); !ok || value != "yes" {
		t.Error("une destination non chiffrée doit accepter l'accès sans question")
	}
	if _, ok := lookup(env, "BORG_PASSCOMMAND"); ok {
		t.Error("aucune commande de mot de passe ne doit être définie sans chiffrement")
	}
}

// TestEnvironnementChiffre vérifie que la passphrase n'est jamais placée dans
// l'environnement : Borg la demande à l'application.
func TestEnvironnementChiffre(t *testing.T) {
	env := Environment{
		Repository:      "ssh://u1@u1.your-storagebox.de:23/./poste",
		Encrypted:       true,
		PassCommandExe:  `C:\Program Files\borgui\borgui.exe`,
		PassCommandArgs: []string{"--print-passphrase", "Poste de Marc"},
	}.environ(toCygwinPath)

	value, ok := lookup(env, "BORG_PASSCOMMAND")
	if !ok {
		t.Fatal("BORG_PASSCOMMAND doit être définie pour une destination chiffrée")
	}
	want := `'/cygdrive/c/Program Files/borgui/borgui.exe' --print-passphrase 'Poste de Marc'`
	if value != want {
		t.Errorf("BORG_PASSCOMMAND = %q, attendu %q", value, want)
	}
	if _, ok := lookup(env, "BORG_PASSPHRASE"); ok {
		t.Error("la passphrase ne doit jamais figurer dans l'environnement")
	}
	if _, ok := lookup(env, "BORG_UNKNOWN_UNENCRYPTED_REPO_ACCESS_IS_OK"); ok {
		t.Error("l'acceptation des dépôts non chiffrés n'a pas lieu d'être ici")
	}
}

// TestTransportSSH vérifie la commande de transport : port de la Storage Box,
// clé dédiée, empreinte épinglée et aucune invite possible.
func TestTransportSSH(t *testing.T) {
	env := Environment{
		SSHKey:     `C:\Users\marc\AppData\Local\borgui\id_ed25519`,
		KnownHosts: `C:\Users\marc\AppData\Local\borgui\known_hosts`,
		Port:       23,
	}.environ(toCygwinPath)

	rsh, ok := lookup(env, "BORG_RSH")
	if !ok {
		t.Fatal("BORG_RSH doit être définie")
	}
	for _, want := range []string{
		"-p 23",
		"/cygdrive/c/Users/marc/AppData/Local/borgui/id_ed25519",
		"StrictHostKeyChecking=yes",
		"BatchMode=yes",
		"IdentitiesOnly=yes",
	} {
		if !strings.Contains(rsh, want) {
			t.Errorf("BORG_RSH ne contient pas %q: %s", want, rsh)
		}
	}
}

// TestProtectionShell vérifie la protection des chaînes découpées par Borg
// selon les règles du shell.
func TestProtectionShell(t *testing.T) {
	cases := map[string]string{
		"simple":            "simple",
		"avec espace":       "'avec espace'",
		"apostrophe'ici":    `'apostrophe'\''ici'`,
		"":                  "''",
		"/chemin/normal-42": "/chemin/normal-42",
	}
	for in, want := range cases {
		if got := shellQuote(in); got != want {
			t.Errorf("shellQuote(%q) = %q, attendu %q", in, got, want)
		}
	}
}
