package borg

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Timestamp est une date rapportée par Borg.
//
// Borg 1.4 écrit ses horodatages sans indication de fuseau
// (« 2026-09-03T22:14:07.000000 »), forme que le décodage JSON par défaut de
// time.Time, qui attend du RFC 3339, refuse. Les dates sont donc lues comme
// locales, ce qu'elles sont : celles du poste qui a créé la sauvegarde.
type Timestamp struct {
	time.Time
}

// borgLayouts énumère les formes acceptées, de la plus courante à la plus rare.
var borgLayouts = []string{
	"2006-01-02T15:04:05.000000",
	"2006-01-02T15:04:05",
	time.RFC3339Nano,
	time.RFC3339,
}

// UnmarshalJSON lit une date de Borg.
func (t *Timestamp) UnmarshalJSON(data []byte) error {
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw == "" {
		t.Time = time.Time{}
		return nil
	}

	for _, layout := range borgLayouts {
		if parsed, err := time.ParseInLocation(layout, raw, time.Local); err == nil {
			t.Time = parsed
			return nil
		}
	}
	return fmt.Errorf("borg: date illisible %q", raw)
}

// MarshalJSON réécrit la date dans la forme de Borg.
func (t Timestamp) MarshalJSON() ([]byte, error) {
	if t.IsZero() {
		return []byte(`""`), nil
	}
	return []byte(`"` + t.Format(borgLayouts[0]) + `"`), nil
}

// String rend la date sans le fuseau, comme Borg l'écrit.
func (t Timestamp) String() string {
	if t.IsZero() {
		return ""
	}
	return strings.TrimSuffix(t.Format(borgLayouts[0]), ".000000")
}
