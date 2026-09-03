package borg

import (
	"errors"
	"strings"
)

var (
	// ErrEngineMissing signale l'absence du moteur de sauvegarde. Sous Linux,
	// l'interface propose alors la commande d'installation de la distribution
	// (EF-08) ; sous Windows, le téléchargement du runtime.
	ErrEngineMissing = errors.New("borg: moteur de sauvegarde introuvable")
	// ErrEngineUnusable signale un moteur présent mais qui ne répond pas comme
	// attendu.
	ErrEngineUnusable = errors.New("borg: moteur de sauvegarde inutilisable")
)

// Failure classe les échecs de Borg que l'application sait expliquer et, pour
// certains, réparer. La classification s'appuie sur l'identifiant de message
// plutôt que sur le texte anglais, qui change d'une version à l'autre.
type Failure int

const (
	// FailureUnknown : aucun diagnostic connu, le journal brut fait foi.
	FailureUnknown Failure = iota
	// FailureRepositoryLocked : verrou laissé par une exécution interrompue.
	// L'application propose de le lever (EF-58).
	FailureRepositoryLocked
	// FailureRepositoryMissing : le dépôt n'existe pas encore.
	FailureRepositoryMissing
	// FailureRepositoryExists : un dépôt occupe déjà cet emplacement.
	FailureRepositoryExists
	// FailurePassphraseWrong : passphrase refusée par le dépôt.
	FailurePassphraseWrong
	// FailureConnection : la destination n'a pas pu être jointe.
	FailureConnection
)

// TranslationKey retourne la clé de traduction du diagnostic. Le catalogue en
// donne l'explication en français et l'action proposée (EI-04).
func (f Failure) TranslationKey() string {
	switch f {
	case FailureRepositoryLocked:
		return "error.repository_locked"
	case FailureRepositoryMissing:
		return "error.repository_missing"
	case FailureRepositoryExists:
		return "error.repository_exists"
	case FailurePassphraseWrong:
		return "error.passphrase_wrong"
	case FailureConnection:
		return "error.connection"
	default:
		return "error.unknown"
	}
}

// failureByMsgID associe les identifiants de message de Borg aux diagnostics.
var failureByMsgID = map[string]Failure{
	"LockTimeout":                      FailureRepositoryLocked,
	"LockFailed":                       FailureRepositoryLocked,
	"LockErrorT":                       FailureRepositoryLocked,
	"Repository.DoesNotExist":          FailureRepositoryMissing,
	"Repository.InvalidRepository":     FailureRepositoryMissing,
	"Repository.AlreadyExists":         FailureRepositoryExists,
	"Cache.RepositoryAccessAborted":    FailureConnection,
	"Repository.ObjectNotFound":        FailureUnknown,
	"PassphraseWrong":                  FailurePassphraseWrong,
	"Connection closed by remote host": FailureConnection,
	"ConnectionClosed":                 FailureConnection,
	"ConnectionClosedWithHint":         FailureConnection,
}

// Diagnose classe l'échec d'un résultat. Le second retour vaut false quand
// l'exécution n'a pas échoué.
func (r *Result) Diagnose() (Failure, bool) {
	if r == nil || r.Status != StatusError {
		return FailureUnknown, false
	}
	for _, m := range r.Messages {
		if f, ok := failureByMsgID[m.MsgID]; ok && f != FailureUnknown {
			return f, true
		}
	}
	// Repli sur le texte : Borg n'attache pas d'identifiant à tous ses
	// messages, notamment ceux venant du transport SSH.
	text := strings.ToLower(joinMessages(r.Messages))
	switch {
	case strings.Contains(text, "failed to create/acquire the lock"),
		strings.Contains(text, "lock.exclusive"):
		return FailureRepositoryLocked, true
	case strings.Contains(text, "does not exist"):
		return FailureRepositoryMissing, true
	case strings.Contains(text, "connection closed"),
		strings.Contains(text, "connection refused"),
		strings.Contains(text, "name or service not known"),
		strings.Contains(text, "permission denied (publickey"):
		return FailureConnection, true
	}
	return FailureUnknown, true
}

// Warnings retourne les messages d'avertissement, dont la liste des fichiers
// que Borg n'a pas pu lire (EF-56).
func (r *Result) Warnings() []Message {
	if r == nil {
		return nil
	}
	var warnings []Message
	for _, m := range r.Messages {
		if strings.EqualFold(m.Level, "WARNING") || strings.EqualFold(m.Level, "ERROR") {
			warnings = append(warnings, m)
		}
	}
	return warnings
}
