package borg

import (
	"errors"
	"strings"
	"testing"
)

// TestDiagnosticsRecette fixe les signatures des deux anomalies relevées en
// recette v0.1 sous Windows, telles que Borg les a rapportées.
func TestDiagnosticsRecette(t *testing.T) {
	cases := []struct {
		name     string
		messages []Message
		want     Failure
	}{
		{"clé trop ouverte", []Message{
			{Level: "ERROR", Text: "Remote: @         WARNING: UNPROTECTED PRIVATE KEY FILE!          @"},
			{Level: "ERROR", Text: "Permissions 0750 for '/cygdrive/c/Users/lucie/AppData/Local/borgui/id_ed25519' are too open."},
			{Level: "ERROR", Text: "u525238-sub5@u525238-sub5.your-storagebox.de: Permission denied (publickey,password)."},
		}, FailureKeyPermissions},
		{"exécutable natif depuis /cygdrive", []Message{
			{Level: "ERROR", Text: "Error: Current working directory is a virtual Cygwin directory which does not exist for a native Windows application."},
			{Level: "ERROR", Text: "NotADirectoryError: [Errno 20] Not a directory: '/cygdrive/c/Users/lucie/Downloads/windows-amd64/borgui.exe'"},
		}, FailureNativeLaunch},
		{"clé simplement refusée", []Message{
			{Level: "ERROR", Text: "Permission denied (publickey,password)."},
		}, FailureConnection},
	}
	for _, c := range cases {
		got, failed := (&Result{Status: StatusError, Messages: c.messages}).Diagnose()
		if !failed || got != c.want {
			t.Errorf("%s: %v, attendu %v", c.name, got, c.want)
		}
	}
}

// TestErreurTypee vérifie que l'échec d'une commande reste identifiable après
// avoir été enveloppé, et que son texte complet garde les messages bruts pour
// le journal.
func TestErreurTypee(t *testing.T) {
	result := &Result{Status: StatusError, ExitCode: 2, Messages: []Message{{Level: "ERROR", MsgID: "LockTimeout", Text: "trace…"}}}
	err := failure(result, "create")

	var failed *CommandError
	if !errors.As(err, &failed) || failed.Diagnosis != FailureRepositoryLocked {
		t.Fatalf("erreur %v", err)
	}
	if !strings.Contains(err.Error(), "trace…") || !strings.Contains(err.Error(), "code 2") {
		t.Errorf("texte complet: %s", err)
	}
}
