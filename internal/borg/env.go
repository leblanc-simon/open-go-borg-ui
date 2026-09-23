package borg

import (
	"fmt"
	"os"
	"strings"
)

// Environment décrit le contexte d'exécution de Borg : dépôt, transport SSH et
// secrets. Les chemins y sont donnés dans la forme native du système ; le
// Runner les traduit avant de les exposer à Borg.
type Environment struct {
	// Repository est l'URL du dépôt, transmise par BORG_REPO.
	Repository string
	// RemotePath est le nom de l'exécutable Borg côté serveur. Hetzner en
	// installe deux et recommande de toujours l'expliciter (EF-22).
	RemotePath string

	// SSHKey est la clé privée dédiée à l'application.
	SSHKey string
	// KnownHosts est le fichier d'empreintes propre à l'application (SEC-04).
	KnownHosts string
	// Port est le port SSH de la destination. 23 pour une Storage Box.
	Port int

	// Encrypted indique si le dépôt exige une passphrase.
	Encrypted bool
	// PassCommandExe est l'exécutable que Borg lance pour obtenir la
	// passphrase, et PassCommandArgs ses arguments. L'application s'invoque
	// elle-même : la passphrase ne transite jamais par l'environnement ni par
	// une ligne de commande (SEC-05).
	PassCommandExe  string
	PassCommandArgs []string

	// BaseDir isole le cache et la configuration de Borg dans les données de
	// l'application plutôt que dans ~/.config/borg.
	BaseDir string

	// UploadRateLimit borne le débit montant en kio/s, 0 pour illimité.
	UploadRateLimit int
}

// environ construit l'environnement du processus Borg. translate convertit un
// chemin natif dans la forme comprise par l'exécutable ; nativePath convient
// pour un Borg natif. wrapNative, s'il est fourni, enveloppe les commandes que
// Borg lance et qui désignent un exécutable natif du système.
func (e Environment) environ(translate func(string) string, wrapNative func(string) string) []string {
	env := os.Environ()

	set := func(key, value string) {
		if value != "" {
			env = append(env, key+"="+value)
		}
	}

	set("BORG_REPO", e.Repository)
	set("BORG_RSH", e.rsh(translate))
	if e.BaseDir != "" {
		set("BORG_BASE_DIR", translate(e.BaseDir))
	}

	if e.Encrypted {
		command := e.passCommand(translate)
		if command != "" && wrapNative != nil {
			command = wrapNative(command)
		}
		set("BORG_PASSCOMMAND", command)
	} else {
		// Sans cette variable, Borg pose une question interactive au premier
		// accès à un dépôt non chiffré et toute sauvegarde planifiée reste
		// bloquée indéfiniment. C'est le piège le plus coûteux du mode non
		// chiffré (EF-38).
		set("BORG_UNKNOWN_UNENCRYPTED_REPO_ACCESS_IS_OK", "yes")
	}
	// Un dépôt déplacé ou renommé ne doit pas non plus suspendre une exécution
	// planifiée sur une question.
	set("BORG_RELOCATED_REPO_ACCESS_IS_OK", "yes")

	return env
}

// passCommand construit la commande de récupération de la passphrase. Borg la
// découpe selon les règles du shell : le chemin de l'exécutable, qui contient
// souvent des espaces sous Windows, est protégé.
func (e Environment) passCommand(translate func(string) string) string {
	if e.PassCommandExe == "" {
		return ""
	}
	parts := []string{shellQuote(translate(e.PassCommandExe))}
	for _, arg := range e.PassCommandArgs {
		parts = append(parts, shellQuote(arg))
	}
	return strings.Join(parts, " ")
}

// rsh construit la commande de transport transmise à Borg. Les chemins sont
// protégés par des apostrophes : Borg découpe cette chaîne selon les règles du
// shell POSIX, y compris sous Cygwin.
func (e Environment) rsh(translate func(string) string) string {
	if e.SSHKey == "" && e.KnownHosts == "" && e.Port == 0 {
		return ""
	}

	parts := []string{"ssh"}
	if e.Port != 0 {
		parts = append(parts, "-p", fmt.Sprint(e.Port))
	}
	if e.SSHKey != "" {
		parts = append(parts, "-i", shellQuote(translate(e.SSHKey)))
		// La clé de l'application est la seule à présenter : sans cette
		// option, un agent SSH chargé de plusieurs clés peut épuiser les
		// tentatives avant d'y arriver.
		parts = append(parts, "-o", "IdentitiesOnly=yes")
	}
	if e.KnownHosts != "" {
		parts = append(parts, "-o", "UserKnownHostsFile="+shellQuote(translate(e.KnownHosts)))
		// L'empreinte a été épinglée au premier appariement : toute
		// divergence ultérieure doit faire échouer la connexion (EF-26).
		parts = append(parts, "-o", "StrictHostKeyChecking=yes")
	}
	// Aucune invite, jamais : une sauvegarde planifiée doit échouer plutôt que
	// d'attendre une saisie.
	parts = append(parts, "-o", "BatchMode=yes")

	return strings.Join(parts, " ")
}

// shellQuote protège une chaîne pour un découpage à la POSIX.
func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	if !strings.ContainsAny(s, " \t\n\"'\\$`&|;<>()*?[]{}~#!") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
