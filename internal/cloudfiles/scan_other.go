//go:build !windows

package cloudfiles

import "context"

// scan ne trouve rien hors de Windows : les clients de synchronisation Linux
// n'exposent pas d'état « à la demande » par un attribut de fichier.
func scan(context.Context, []string) ([]string, error) { return nil, nil }
