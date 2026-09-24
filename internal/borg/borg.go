// Package borg pilote l'exécutable BorgBackup.
//
// Toute exécution de Borg passe par l'interface Runner (AR-01) : ni l'interface
// graphique ni le reste du cœur applicatif ne construisent de ligne de commande.
// Deux implémentations existent, NativeRunner sous Linux et CygwinRunner sous
// Windows, et la traduction des chemins leur appartient exclusivement (AR-02).
package borg

import (
	"context"
	"time"
)

// Status résume l'issue d'une exécution.
//
// Borg distingue trois codes de retour, et confondre l'avertissement avec
// l'échec est le contresens le plus coûteux du projet : un code 1 signale des
// fichiers illisibles, mais la sauvegarde existe et elle est exploitable
// (EF-56).
type Status int

const (
	// StatusSuccess : code 0, exécution complète.
	StatusSuccess Status = iota
	// StatusWarning : code 1, terminé avec des avertissements.
	StatusWarning
	// StatusError : code 2 ou supérieur, échec.
	StatusError
)

// String retourne la clé de traduction du statut, jamais un libellé.
func (s Status) String() string {
	switch s {
	case StatusSuccess:
		return "status.success"
	case StatusWarning:
		return "status.warning"
	default:
		return "status.error"
	}
}

// statusFromExitCode classe un code de retour Borg.
func statusFromExitCode(code int) Status {
	switch {
	case code == 0:
		return StatusSuccess
	case code == 1:
		return StatusWarning
	default:
		return StatusError
	}
}

// PathMode indique la convention de chemins que le Runner doit appliquer aux
// sources de la commande.
type PathMode int

const (
	// PathNone : la commande ne manipule aucun chemin local.
	PathNone PathMode = iota
	// PathSources : chemins à sauvegarder, tels que « borg create » les attend.
	PathSources
	// PathExtract : chemins à extraire, tels que « borg extract » les attend.
	PathExtract
)

// Command décrit une invocation de Borg indépendamment de la plateforme.
//
// Les chemins de Sources sont donnés dans la forme native du système
// (C:\Users\marc\Documents ou /home/marc/Documents) : leur traduction relève
// du Runner.
type Command struct {
	// Name est la sous-commande Borg (init, create, list, info…).
	Name string
	// Flags sont les options déjà formées, hors --remote-path et hors options
	// de journalisation, ajoutées par le Runner.
	Flags []string
	// Target est le dépôt ou le dépôt suivi de ::archive. Vide, la commande
	// s'appuie sur BORG_REPO.
	Target string
	// Sources sont les chemins natifs manipulés par la commande.
	Sources []string
	// Dir est le répertoire de travail natif imposé à la commande, utilisé
	// par l'extraction pour décider où atterrissent les fichiers restaurés.
	// Vide, le Runner choisit selon la convention de la plateforme.
	Dir string
	// PathMode choisit la convention appliquée aux Sources.
	PathMode PathMode
	// ExcludePaths sont des chemins natifs précis à écarter, calculés au
	// moment de l'exécution — fichiers à la demande d'un service de
	// stockage en ligne, par exemple. Le Runner les traduit dans la forme
	// de l'archive, comme les Sources ; ils ne sont jamais enregistrés dans
	// la configuration, où seuls des motifs portables ont leur place.
	ExcludePaths []string
	// Env est l'environnement Borg de l'exécution.
	Env Environment
	// LogJSON demande les événements de progression sur stderr. Le Runner les
	// transmet à OnEvent au fil de l'eau.
	LogJSON bool
	// OnEvent reçoit les événements de progression. Peut être nil.
	OnEvent func(Event)
}

// Result est l'issue d'une exécution.
type Result struct {
	Status   Status
	ExitCode int
	// Stdout est la sortie standard complète, généralement du JSON.
	Stdout []byte
	// Messages regroupe les messages de journal émis par Borg, dans l'ordre.
	Messages []Message
	Duration time.Duration
	// CommandLine est la ligne réellement exécutée, sans aucun secret, pour
	// le journal de support.
	CommandLine string
}

// Runner exécute des commandes Borg.
type Runner interface {
	// Run exécute la commande et attend son terme. Une erreur n'est retournée
	// que si Borg n'a pas pu être lancé ou si le contexte a été annulé : un
	// code de retour non nul se lit dans Result.
	Run(ctx context.Context, cmd Command) (*Result, error)
	// Version retourne la version de Borg rapportée par l'exécutable.
	Version(ctx context.Context) (string, error)
	// Executable retourne le chemin de l'exécutable utilisé, pour le
	// diagnostic.
	Executable() string
	// Origin traduit un chemin tel qu'il figure dans une archive en chemin
	// natif du poste — l'emplacement d'où il a été sauvegardé — et donne le
	// répertoire depuis lequel l'extraire pour qu'il y retourne. La
	// convention de chemins reste ainsi l'affaire du seul Runner (AR-02).
	Origin(archivePath string) (native, root string, err error)
}
