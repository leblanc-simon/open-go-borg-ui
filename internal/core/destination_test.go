package core

import (
	"context"
	"errors"
	"testing"

	"leblanc.io/open-go-borg-ui/internal/borg"
)

// scriptedRunner répond par un résultat fixé et retient l'environnement reçu.
type scriptedRunner struct {
	result *borg.Result
	env    borg.Environment
	name   string
}

func (r *scriptedRunner) Run(_ context.Context, cmd borg.Command) (*borg.Result, error) {
	r.env, r.name = cmd.Env, cmd.Name
	return r.result, nil
}
func (r *scriptedRunner) Version(context.Context) (string, error) { return "1.4.5", nil }
func (r *scriptedRunner) Executable() string                      { return "borg" }

// failed construit un échec de Borg portant un identifiant de message.
func failed(msgid string) *borg.Result {
	return &borg.Result{Status: borg.StatusError, ExitCode: 2, Messages: []borg.Message{{Level: "ERROR", MsgID: msgid}}}
}

// TestInspection vérifie les trois situations d'une destination inconnue, et
// que l'interrogation se fait en sonde.
func TestInspection(t *testing.T) {
	cases := []struct {
		name   string
		result *borg.Result
		want   DestinationState
	}{
		{"vide", failed("Repository.DoesNotExist"), DestinationState{}},
		{"chiffrée", failed("PassphraseWrong"), DestinationState{Exists: true, Encrypted: true}},
		{"non chiffrée", &borg.Result{Status: borg.StatusSuccess,
			Stdout: []byte(`{"encryption":{"mode":"none"},"cache":{"stats":{"unique_csize":1024}}}`)},
			DestinationState{Exists: true, Mode: "none", Size: 1024}},
	}
	for _, c := range cases {
		runner := &scriptedRunner{result: c.result}
		got, err := InspectDestination(context.Background(), runner, borg.Environment{Encrypted: true})
		if err != nil || got != c.want {
			t.Errorf("%s: %+v, erreur %v", c.name, got, err)
		}
		if !runner.env.Probe {
			t.Errorf("%s: l'interrogation doit se faire en sonde", c.name)
		}
	}

	runner := &scriptedRunner{result: failed("ConnectionClosed")}
	if _, err := InspectDestination(context.Background(), runner, borg.Environment{}); err == nil {
		t.Error("une destination injoignable doit être une erreur")
	}
}

// TestPassphraseRefusee vérifie la vérification d'une passphrase saisie.
func TestPassphraseRefusee(t *testing.T) {
	runner := &scriptedRunner{result: failed("PassphraseWrong")}
	if _, err := VerifyPassphrase(context.Background(), runner, borg.Environment{Encrypted: true}); !errors.Is(err, ErrPassphraseWrong) {
		t.Errorf("erreur %v, attendu ErrPassphraseWrong", err)
	}
	if runner.env.Probe {
		t.Error("la vérification doit utiliser la vraie passphrase")
	}

	runner = &scriptedRunner{result: &borg.Result{Status: borg.StatusSuccess,
		Stdout: []byte(`{"encryption":{"mode":"repokey-blake2"}}`)}}
	state, err := VerifyPassphrase(context.Background(), runner, borg.Environment{Encrypted: true})
	if err != nil || state.Mode != "repokey-blake2" || !state.Encrypted {
		t.Errorf("état %+v, erreur %v", state, err)
	}
}

// TestCreation vérifie la création dans le mode choisi.
func TestCreation(t *testing.T) {
	runner := &scriptedRunner{result: &borg.Result{Status: borg.StatusSuccess}}
	if err := CreateDestination(context.Background(), runner, borg.Environment{}, "repokey-blake2"); err != nil || runner.name != "init" {
		t.Errorf("création: %v, commande %s", err, runner.name)
	}
	runner = &scriptedRunner{result: failed("Repository.AlreadyExists")}
	var cmdErr *borg.CommandError
	if err := CreateDestination(context.Background(), runner, borg.Environment{}, "none"); !errors.As(err, &cmdErr) ||
		cmdErr.Diagnosis != borg.FailureRepositoryExists {
		t.Errorf("destination déjà présente: %v", err)
	}
}
