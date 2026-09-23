package main

import (
	"context"
	"errors"
	"fmt"

	"leblanc.io/open-go-borg-ui/internal/borg"
	"leblanc.io/open-go-borg-ui/internal/config"
	"leblanc.io/open-go-borg-ui/internal/core"
	"leblanc.io/open-go-borg-ui/internal/lock"
	"leblanc.io/open-go-borg-ui/internal/secret"
)

// commandRepository crée le dépôt ou en rend compte.
func (a *app) commandRepository(ctx context.Context, args []string) (int, error) {
	action, _ := subcommand(args)
	switch action {
	case "init":
		return a.repositoryInit(ctx)
	case "info":
		return a.repositoryInfo(ctx)
	case "export-key":
		return a.repositoryExportKey(ctx)
	case "unlock":
		return a.repositoryUnlock(ctx)
	default:
		return exitError, fmt.Errorf("%s", a.T("repository.usage"))
	}
}

// repositoryInit crée le dépôt dans le mode de chiffrement du profil.
//
// Le mode est figé ici pour toute la vie du dépôt : en changer imposerait d'en
// créer un autre et de perdre l'historique (EF-32).
func (a *app) repositoryInit(ctx context.Context) (int, error) {
	profile, err := a.profile()
	if err != nil {
		return exitError, err
	}
	runner, err := a.runner()
	if err != nil {
		return a.reportMissingEngine(err)
	}
	env, err := a.environment(profile)
	if err != nil {
		return exitError, err
	}

	if profile.Encryption.Encrypted() {
		// Sans passphrase enregistrée, la création s'arrêterait sur une
		// demande de saisie que rien ne peut satisfaire dans une exécution non
		// interactive.
		if _, err := a.secrets().Get(profile.Name); err != nil {
			return exitError, fmt.Errorf("%s", a.T("repository.passphrase_required"))
		}
	}

	fmt.Println(a.T("repository.creating", map[string]any{
		"Repository": env.Repository,
		"Mode":       a.encryptionLabel(string(profile.Encryption)),
	}))

	result, err := borg.Init(ctx, runner, borg.InitOptions{
		Env:  env,
		Mode: profile.Encryption.BorgMode(),
	})
	if err != nil {
		return exitError, err
	}
	if result.Status == borg.StatusError {
		if diagnosis, ok := result.Diagnose(); ok && diagnosis == borg.FailureRepositoryExists {
			return exitError, fmt.Errorf("%s", a.T("repository.already_exists"))
		}
		return exitError, a.borgFailure(result)
	}

	fmt.Println(a.T("repository.created"))
	if profile.Encryption.Encrypted() {
		// La clé de secours est la seule protection contre la perte de la
		// passphrase, et l'exporter est obligatoire avant la première
		// sauvegarde (EF-35).
		fmt.Println(a.T("repository.export_key_required"))
	}
	return exitSuccess, nil
}

// repositoryInfo affiche l'état du dépôt.
//
// Le mode de chiffrement est lu ici, jamais demandé à l'utilisateur (EF-34).
func (a *app) repositoryInfo(ctx context.Context) (int, error) {
	profile, err := a.profile()
	if err != nil {
		return exitError, err
	}
	runner, err := a.runner()
	if err != nil {
		return a.reportMissingEngine(err)
	}
	env, err := a.environment(profile)
	if err != nil {
		return exitError, err
	}

	info, _, err := borg.Info(ctx, runner, env)
	if err != nil {
		return exitError, err
	}

	fmt.Println(a.T("repository.location", map[string]any{"Repository": info.Repository.Location}))
	fmt.Println(a.T("repository.mode", map[string]any{"Mode": a.encryptionLabel(info.Encryption.Mode)}))
	fmt.Println(a.T("repository.size", map[string]any{
		"Size":     formatSize(info.Cache.Stats.UniqueCSize),
		"Original": formatSize(info.Cache.Stats.TotalSize),
	}))

	// Le mode constaté fait foi : si la configuration en annonce un autre,
	// c'est elle qui a tort.
	if info.Encryption.Mode != "" && info.Encryption.Mode != string(profile.Encryption) {
		fmt.Println(a.T("repository.mode_differs", map[string]any{
			"Configured": a.encryptionLabel(string(profile.Encryption)),
			"Actual":     a.encryptionLabel(info.Encryption.Mode),
		}))
		return exitWarning, nil
	}
	return exitSuccess, nil
}

// repositoryExportKey affiche la clé de secours du dépôt.
func (a *app) repositoryExportKey(ctx context.Context) (int, error) {
	profile, err := a.profile()
	if err != nil {
		return exitError, err
	}
	if !profile.Encryption.Encrypted() {
		// Sans chiffrement, il n'y a pas de clé : l'étape n'existe pas (EF-39).
		fmt.Println(a.T("repository.no_key"))
		return exitSuccess, nil
	}

	runner, err := a.runner()
	if err != nil {
		return a.reportMissingEngine(err)
	}
	env, err := a.environment(profile)
	if err != nil {
		return exitError, err
	}

	result, err := runner.Run(ctx, borg.Command{
		Name:    "key",
		Flags:   []string{"export", "--paper"},
		Env:     env,
		LogJSON: true,
	})
	if err != nil {
		return exitError, err
	}
	if result.Status == borg.StatusError {
		return exitError, a.borgFailure(result)
	}

	fmt.Println(a.T("repository.key_intro"))
	fmt.Println()
	fmt.Print(string(result.Stdout))
	fmt.Println()
	fmt.Println(a.T("repository.key_warning"))
	return exitSuccess, nil
}

// repositoryUnlock lève le verrou laissé sur la destination par une
// sauvegarde interrompue brutalement (EF-58).
//
// Le verrou local est pris d'abord : s'il est tenu, une exécution est en
// cours sur ce poste, et le verrou de la destination est légitime.
func (a *app) repositoryUnlock(ctx context.Context) (int, error) {
	profile, err := a.profile()
	if err != nil {
		return exitError, err
	}
	if err := a.ensurePassphrase(profile); err != nil {
		return exitError, err
	}
	runner, err := a.runner()
	if err != nil {
		return a.reportMissingEngine(err)
	}
	env, err := a.environment(profile)
	if err != nil {
		return exitError, err
	}

	held, err := lock.Acquire(lock.PathFor(a.lockDir(), env.Repository))
	if errors.Is(err, lock.ErrBusy) {
		return exitError, fmt.Errorf("%s", a.T(core.ErrorKeyAlreadyRunning))
	}
	if err != nil {
		return exitError, err
	}
	defer held.Release()

	result, err := borg.BreakLock(ctx, runner, env)
	if err != nil {
		if result != nil {
			return exitError, a.borgFailure(result)
		}
		return exitError, err
	}
	fmt.Println(a.T("repository.unlocked"))
	return exitSuccess, nil
}

// borgFailure traduit l'échec d'une commande Borg en message compréhensible,
// le journal brut restant disponible (EI-04).
func (a *app) borgFailure(result *borg.Result) error {
	diagnosis, _ := result.Diagnose()
	message := a.T(diagnosis.TranslationKey())
	for _, warning := range result.Warnings() {
		message += "\n" + a.T("cli.details", map[string]any{"Message": warning.Text})
	}
	return errors.New(message)
}

// ensurePassphrase vérifie qu'une passphrase est disponible pour un profil
// chiffré.
func (a *app) ensurePassphrase(profile *config.Profile) error {
	if !profile.Encryption.Encrypted() {
		return nil
	}
	if _, err := a.secrets().Get(profile.Name); err != nil {
		if errors.Is(err, secret.ErrNotFound) {
			return errors.New(a.T("passphrase.missing"))
		}
		return err
	}
	return nil
}
