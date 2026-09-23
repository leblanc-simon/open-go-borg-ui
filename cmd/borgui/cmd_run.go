package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"leblanc.io/open-go-borg-ui/internal/config"
)

// maxLogSize borne le journal d'un profil. Au-delà, il est renommé en .1 et
// un nouveau commence : deux générations suffisent à comprendre un incident
// récent sans laisser le fichier grossir indéfiniment.
const maxLogSize = 5 << 20

// runScheduled est le mode qu'invoque la tâche planifiée (EF-63) :
// « borgui --run <profil> ».
//
// Personne ne regarde le terminal : toute la sortie part dans le journal du
// profil, et le code de sortie suit la convention de Borg — 0 succès,
// 1 avertissements, 2 échec. Aucune question ne peut être posée, ce que
// garantissent la passphrase lue dans le trousseau et, sans chiffrement,
// BORG_UNKNOWN_UNENCRYPTED_REPO_ACCESS_IS_OK (EF-38).
func (a *app) runScheduled(ctx context.Context) int {
	log, err := a.openLog()
	if err != nil {
		// Sans journal, l'exécution a quand même lieu : mieux vaut une
		// sauvegarde muette que pas de sauvegarde.
		fmt.Fprintln(os.Stderr, err)
	} else {
		defer log.Close()
		os.Stdout, os.Stderr, stderr = log, log, log
	}

	fmt.Println(a.T("run.header", map[string]any{
		"Date":    time.Now().Format("2006-01-02 15:04:05"),
		"Profile": a.profileName,
	}))

	code, err := a.dispatch(ctx, "backup", nil)
	if err != nil {
		fmt.Println(a.T("cli.error", map[string]any{"Message": err.Error()}))
	}
	fmt.Println(a.T("run.footer", map[string]any{"Code": code}))
	return code
}

// openLog ouvre le journal du profil en ajout, après rotation s'il est plein.
func (a *app) openLog() (*os.File, error) {
	path := a.logPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if info, err := os.Stat(path); err == nil && info.Size() > maxLogSize {
		os.Rename(path, path+".1")
	}
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
}

// logPath retourne le journal des exécutions planifiées du profil.
func (a *app) logPath() string {
	return filepath.Join(a.stateDir, "logs", config.SafeName(a.profileName)+".log")
}
