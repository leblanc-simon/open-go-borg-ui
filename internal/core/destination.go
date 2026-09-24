package core

import (
	"context"
	"errors"

	"leblanc.io/open-go-borg-ui/internal/borg"
)

// DestinationState est ce que l'on sait d'une destination avant de s'en
// servir.
type DestinationState struct {
	// Exists : la destination contient déjà des sauvegardes.
	Exists bool
	// Encrypted et Mode décrivent son chiffrement, lu et jamais redemandé
	// (EF-34). Mode reste vide tant que la passphrase n'est pas connue.
	Encrypted bool
	Mode      string
	// Size est l'espace occupé, quand il a pu être lu.
	Size int64
}

// ErrPassphraseWrong signale une passphrase refusée par la destination.
var ErrPassphraseWrong = errors.New("core: passphrase refusée par la destination")

// InspectDestination interroge une destination dont on ignore tout, sans
// passphrase : une destination chiffrée se reconnaît à ce qu'elle la refuse.
func InspectDestination(ctx context.Context, runner borg.Runner, env borg.Environment) (DestinationState, error) {
	env.Probe = true
	info, result, err := borg.Info(ctx, runner, env)
	if err == nil {
		return DestinationState{
			Exists:    true,
			Encrypted: info.Encryption.Mode != "none",
			Mode:      info.Encryption.Mode,
			Size:      info.Cache.Stats.UniqueCSize,
		}, nil
	}
	if result != nil {
		switch diagnosis, _ := result.Diagnose(); diagnosis {
		case borg.FailureRepositoryMissing:
			return DestinationState{}, nil
		case borg.FailurePassphraseWrong:
			return DestinationState{Exists: true, Encrypted: true}, nil
		}
	}
	return DestinationState{}, err
}

// CreateDestination crée la destination dans le mode choisi, figé pour sa vie
// entière (EF-32). Une destination apparue entre-temps n'est pas écrasée :
// elle est signalée comme existante.
func CreateDestination(ctx context.Context, runner borg.Runner, env borg.Environment, mode string) error {
	result, err := borg.Init(ctx, runner, borg.InitOptions{Env: env, Mode: mode})
	if err != nil {
		return err
	}
	if result.Status == borg.StatusError {
		return &borg.CommandError{Name: "init", ExitCode: result.ExitCode, Diagnosis: diagnosis(result), Messages: result.Messages}
	}
	return nil
}

// VerifyPassphrase vérifie que la passphrase enregistrée ouvre la
// destination, et retourne son état exact.
func VerifyPassphrase(ctx context.Context, runner borg.Runner, env borg.Environment) (DestinationState, error) {
	info, result, err := borg.Info(ctx, runner, env)
	if err != nil {
		if result != nil && diagnosis(result) == borg.FailurePassphraseWrong {
			return DestinationState{}, ErrPassphraseWrong
		}
		return DestinationState{}, err
	}
	return DestinationState{
		Exists:    true,
		Encrypted: info.Encryption.Mode != "none",
		Mode:      info.Encryption.Mode,
		Size:      info.Cache.Stats.UniqueCSize,
	}, nil
}

// diagnosis classe l'échec d'un résultat.
func diagnosis(result *borg.Result) borg.Failure {
	failure, _ := result.Diagnose()
	return failure
}
