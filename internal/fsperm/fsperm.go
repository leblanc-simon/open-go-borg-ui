// Package fsperm réserve au seul propriétaire les fichiers sensibles de
// l'application : clé SSH privée, empreintes épinglées, passphrase de repli.
//
// Sous Windows, le mode passé à os.WriteFile est ignoré : le fichier hérite de
// la DACL de son dossier, qui accorde toujours l'accès à SYSTEM et aux
// administrateurs. Cygwin traduit ces entrées en bits de groupe, et le ssh du
// runtime refuse alors la clé comme trop ouverte (anomalie relevée en recette
// v0.1). Seule une DACL explicite, protégée de l'héritage, règle la question.
package fsperm

import (
	"fmt"
	"os"
)

// WritePrivate écrit data dans path, lisible et modifiable par le seul
// propriétaire.
//
// Le fichier est d'abord créé vide et restreint, puis rempli : le contenu
// n'est à aucun moment lisible avec des droits plus larges.
func WritePrivate(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("fsperm: %w", err)
	}
	file.Close()
	if err := Restrict(path); err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("fsperm: %w", err)
	}
	return nil
}

// Restrict réserve un fichier existant au seul propriétaire. L'opération est
// idempotente et peu coûteuse : elle se réapplique à chaque usage, ce qui
// répare un fichier restauré, copié à la main ou écrit par une version
// antérieure de l'application.
func Restrict(path string) error {
	if err := restrict(path); err != nil {
		return fmt.Errorf("fsperm: %s: %w", path, err)
	}
	return nil
}
