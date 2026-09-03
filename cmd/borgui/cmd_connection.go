package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"time"

	"leblanc.io/open-go-borg-ui/internal/borg"
	"leblanc.io/open-go-borg-ui/internal/config"
	"leblanc.io/open-go-borg-ui/internal/probe"
)

// commandConnection diagnostique la destination.
func (a *app) commandConnection(ctx context.Context, args []string) (int, error) {
	action, rest := subcommand(args)
	if action != "" && action != "test" {
		return exitError, fmt.Errorf("%s", a.T("connection.usage"))
	}

	var pin bool
	flags := flag.NewFlagSet("connection test", flag.ContinueOnError)
	flags.BoolVar(&pin, "pin", false,
		"accepte et enregistre l'empreinte du serveur lors du premier appariement")
	if err := flags.Parse(rest); err != nil {
		return exitError, err
	}

	profile, err := a.profile()
	if err != nil {
		return exitError, err
	}
	repository, err := profile.Destination.RepositoryURL()
	if err != nil {
		return exitError, err
	}
	keyPath, err := a.sshKeyPath(profile)
	if err != nil {
		return exitError, err
	}
	knownHosts, err := config.KnownHostsPath()
	if err != nil {
		return exitError, err
	}

	host, port := hostPort(repository)
	user := sshUser(repository)
	if user == "" {
		user = profile.Destination.User
	}

	fmt.Println(a.T("connection.testing", map[string]any{"Host": host, "Port": port}))

	outcomes := probe.Run(ctx, probe.Params{
		Host:           host,
		Port:           port,
		User:           user,
		KeyPath:        keyPath,
		KnownHostsPath: knownHosts,
		AllowPinning:   pin,
		RemotePath:     profile.Destination.RemotePath,
		Timeout:        20 * time.Second,
	})

	for _, outcome := range outcomes {
		a.printOutcome(outcome)
	}

	if failed, ok := probe.Failed(outcomes); ok {
		if errors.Is(failed.Err, probe.ErrHostKeyUnknown) {
			// Le premier appariement est une décision de l'utilisateur : il
			// doit la prendre en connaissance de l'empreinte.
			fmt.Println(a.T("connection.pin_hint"))
		}
		return exitError, fmt.Errorf("%s", a.T(failed.Step.FixKey()))
	}

	// Dernière étape : le dépôt lui-même. Elle passe par Borg, seul capable de
	// dire si le dépôt est lisible avec les paramètres du profil.
	return a.testRepository(ctx, profile)
}

// testRepository vérifie que le dépôt est lisible.
func (a *app) testRepository(ctx context.Context, profile *config.Profile) (int, error) {
	runner, err := a.runner()
	if err != nil {
		return a.reportMissingEngine(err)
	}
	env, err := a.environment(profile)
	if err != nil {
		return exitError, err
	}

	info, result, err := borg.Info(ctx, runner, env)
	if err != nil {
		if result != nil {
			if diagnosis, ok := result.Diagnose(); ok && diagnosis == borg.FailureRepositoryMissing {
				// Un dépôt absent n'est pas une anomalie : c'est l'état d'une
				// destination qui n'a pas encore servi.
				fmt.Println(a.T("connection.step_repository_missing"))
				return exitSuccess, nil
			}
		}
		return exitError, err
	}

	fmt.Println(a.T("connection.step_repository_ok", map[string]any{
		"Size": formatSize(info.Cache.Stats.UniqueCSize),
		"Mode": a.encryptionLabel(info.Encryption.Mode),
	}))
	return exitSuccess, nil
}

// printOutcome affiche le résultat d'une étape du diagnostic.
func (a *app) printOutcome(outcome probe.Outcome) {
	label := a.T(outcome.Step.TranslationKey())
	if outcome.OK {
		detail := outcome.Detail
		if detail == "" {
			fmt.Println(a.T("connection.step_ok", map[string]any{"Step": label}))
			return
		}
		fmt.Println(a.T("connection.step_ok_detail", map[string]any{"Step": label, "Detail": detail}))
		return
	}
	fmt.Println(a.T("connection.step_failed", map[string]any{"Step": label}))
	if outcome.Err != nil {
		fmt.Println(a.T("cli.details", map[string]any{"Message": outcome.Err.Error()}))
	}
}

// encryptionLabel traduit le mode de chiffrement rapporté par Borg. Le
// vocabulaire de Borg ne sort jamais tel quel (EI-02).
func (a *app) encryptionLabel(mode string) string {
	if mode == string(config.EncryptionNone) {
		return a.T("encryption.none")
	}
	return a.T("encryption.encrypted")
}
