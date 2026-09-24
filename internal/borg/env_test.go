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
	}.environ(nativePath, nil)

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
	}.environ(toCygwinPath, nil)

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
	}.environ(toCygwinPath, nil)

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

// shlexSplit découpe une chaîne comme shlex.split en mode POSIX, ce que fait
// Borg de BORG_PASSCOMMAND : apostrophes littérales, guillemets avec
// échappements, barre oblique inverse hors guillemets.
func shlexSplit(t *testing.T, s string) []string {
	t.Helper()
	var words []string
	var word strings.Builder
	inWord := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\'':
			end := strings.IndexByte(s[i+1:], '\'')
			if end < 0 {
				t.Fatalf("apostrophe non fermée: %s", s)
			}
			word.WriteString(s[i+1 : i+1+end])
			i += end + 1
			inWord = true
		case c == '"':
			i++
			for ; i < len(s) && s[i] != '"'; i++ {
				if s[i] == '\\' && i+1 < len(s) && strings.IndexByte("\\\"$`", s[i+1]) >= 0 {
					i++
				}
				word.WriteByte(s[i])
			}
			inWord = true
		case c == '\\' && i+1 < len(s):
			i++
			word.WriteByte(s[i])
			inWord = true
		case c == ' ' || c == '\t':
			if inWord {
				words = append(words, word.String())
				word.Reset()
				inWord = false
			}
		default:
			word.WriteByte(c)
			inWord = true
		}
	}
	if inWord {
		words = append(words, word.String())
	}
	return words
}

// TestPassphraseDepuisCygdrive vérifie le correctif de l'anomalie relevée en
// recette : sous Cygwin, la commande de passphrase est lancée par bash, qui
// quitte /cygdrive avant d'exécuter l'application. La chaîne doit survivre au
// découpage de Borg, espaces et apostrophes compris, et le script obtenu
// doit à son tour désigner l'exécutable et ses arguments sans les altérer.
func TestPassphraseDepuisCygdrive(t *testing.T) {
	env := Environment{
		Repository:      "ssh://u1@u1.your-storagebox.de:23/./poste",
		Encrypted:       true,
		PassCommandExe:  `C:\Users\Jean Dupont\AppData\Local\Programs\l'app\borgui.exe`,
		PassCommandArgs: []string{"--print-passphrase", "Poste de Jean"},
	}.environ(toCygwinPath, cygwinNativeCommand)

	value, _ := lookup(env, "BORG_PASSCOMMAND")
	argv := shlexSplit(t, value)
	if len(argv) != 3 || argv[0] != "/usr/bin/bash" || argv[1] != "-c" {
		t.Fatalf("découpage de Borg: %q", argv)
	}
	script, ok := strings.CutPrefix(argv[2], "cd / && exec ")
	if !ok {
		t.Fatalf("le script ne quitte pas /cygdrive: %q", argv[2])
	}
	got := shlexSplit(t, script)
	want := []string{"/cygdrive/c/Users/Jean Dupont/AppData/Local/Programs/l'app/borgui.exe", "--print-passphrase", "Poste de Jean"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("commande exécutée par bash: %q\nattendu: %q", got, want)
	}
}

// TestPassphraseNative vérifie que Linux n'est pas concerné : aucune
// enveloppe.
func TestPassphraseNative(t *testing.T) {
	env := Environment{Encrypted: true, PassCommandExe: "/usr/local/bin/borgui",
		PassCommandArgs: []string{"--print-passphrase", "poste"}}.environ(nativePath, nil)
	if value, _ := lookup(env, "BORG_PASSCOMMAND"); value != "/usr/local/bin/borgui --print-passphrase poste" {
		t.Errorf("BORG_PASSCOMMAND = %q", value)
	}
}

// TestSonde vérifie l'interrogation d'une destination au chiffrement
// inconnu : une passphrase explicitement vide, jamais de commande de
// passphrase, et l'accès sans question à une destination non chiffrée.
func TestSonde(t *testing.T) {
	t.Setenv("BORG_PASSPHRASE", "héritée-du-shell")
	env := Environment{Probe: true, Encrypted: true, PassCommandExe: "/usr/local/bin/borgui"}.environ(nativePath, nil)

	if value, ok := lookup(env, "BORG_PASSPHRASE"); !ok || value != "" {
		t.Errorf("BORG_PASSPHRASE = %q, attendue vide", value)
	}
	if _, ok := lookup(env, "BORG_PASSCOMMAND"); ok {
		t.Error("une sonde ne doit pas demander la passphrase à l'application")
	}
	if value, _ := lookup(env, "BORG_UNKNOWN_UNENCRYPTED_REPO_ACCESS_IS_OK"); value != "yes" {
		t.Error("une destination non chiffrée doit répondre sans question")
	}
}
