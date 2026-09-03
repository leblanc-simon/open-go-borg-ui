package probe

import (
	"encoding/pem"
	"errors"
	"os"
)

// pemEncode sérialise le bloc PEM d'une clé privée.
func pemEncode(block *pem.Block) []byte { return pem.EncodeToMemory(block) }

// errCause déballe une erreur pour interroger sa cause système.
func errCause(err error) error {
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		return pathErr.Err
	}
	return err
}
