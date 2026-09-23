// Package cloudfiles repère les fichiers « à la demande » des services de
// stockage en ligne (OneDrive, Dropbox, Google Drive…).
//
// Ces fichiers apparaissent dans l'explorateur mais leur contenu n'est pas sur
// le disque : les lire déclenche leur téléchargement complet. Une sauvegarde
// qui les inclurait rapatrierait donc l'intégralité du stockage en ligne sur
// le poste. Ils sont écartés par défaut, et leur nombre est signalé
// (addendum §6.1).
//
// Seul Windows expose cet état par un attribut de fichier ; ailleurs, Scan ne
// trouve jamais rien.
package cloudfiles

import "context"

// Attributs Windows qui signalent un contenu absent du disque.
const (
	// attributeRecallOnDataAccess : le contenu est rapatrié à la première
	// lecture. C'est l'état des fichiers « disponibles en ligne
	// uniquement » de OneDrive et de ses équivalents.
	attributeRecallOnDataAccess = 0x00400000
	// attributeOffline : le contenu n'est pas immédiatement disponible,
	// forme plus ancienne du même état.
	attributeOffline = 0x00001000
)

// isPlaceholder indique si des attributs Windows désignent un fichier dont le
// contenu n'est pas sur le disque.
func isPlaceholder(attributes uint32) bool {
	return attributes&(attributeRecallOnDataAccess|attributeOffline) != 0
}

// Scan parcourt les sources et retourne les fichiers à la demande qu'elles
// contiennent, en chemins natifs.
//
// Le parcours ne lit aucun contenu et ne suit ni liens ni jonctions : il ne
// peut donc lui-même rien rapatrier.
func Scan(ctx context.Context, sources []string) ([]string, error) {
	return scan(ctx, sources)
}
