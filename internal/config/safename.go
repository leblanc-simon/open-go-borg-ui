package config

import (
	"fmt"
	"strings"
)

// SafeName réduit un nom de profil à des caractères admis partout dans un nom
// de fichier ou d'unité systemd. Les autres sont remplacés par « _ » suivi de
// leur code ; « _ » lui-même est échappé, ce qui garde deux noms distincts
// distincts.
func SafeName(name string) string {
	if name == "" {
		return "default"
	}
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-':
			b.WriteRune(r)
		default:
			fmt.Fprintf(&b, "_%x", r)
		}
	}
	return b.String()
}
