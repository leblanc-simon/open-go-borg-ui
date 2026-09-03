package borg

import (
	"encoding/json"
	"testing"
	"time"
)

// TestDatesDeBorg couvre les formes d'horodatage rencontrées dans les sorties
// JSON de Borg, dont la principale n'est pas du RFC 3339 et serait donc
// refusée par le décodage par défaut de time.Time.
func TestDatesDeBorg(t *testing.T) {
	cases := map[string]time.Time{
		`"2026-09-03T22:14:07.000000"`:       time.Date(2026, 9, 3, 22, 14, 7, 0, time.Local),
		`"2026-09-03T22:14:07"`:              time.Date(2026, 9, 3, 22, 14, 7, 0, time.Local),
		`"2026-09-03T22:14:07.500000+02:00"`: time.Date(2026, 9, 3, 22, 14, 7, 500000000, time.FixedZone("", 2*3600)),
	}

	for raw, want := range cases {
		var got Timestamp
		if err := json.Unmarshal([]byte(raw), &got); err != nil {
			t.Errorf("date %s refusée: %v", raw, err)
			continue
		}
		if !got.Equal(want) {
			t.Errorf("date %s = %s, attendue %s", raw, got, want)
		}
	}
}

// TestDateVide vérifie qu'une date absente ne fait pas échouer la lecture de
// toute la réponse.
func TestDateVide(t *testing.T) {
	var got Timestamp
	if err := json.Unmarshal([]byte(`""`), &got); err != nil {
		t.Fatalf("date vide refusée: %v", err)
	}
	if !got.IsZero() {
		t.Errorf("date vide = %s, attendue nulle", got)
	}
}

// TestListeDArchives vérifie la lecture d'une réponse complète de « borg list ».
func TestListeDArchives(t *testing.T) {
	raw := `{"archives":[{"name":"poste-2026-09-03T22:14:07","start":"2026-09-03T22:14:07.000000"}],
	         "encryption":{"mode":"repokey-blake2"}}`

	var list ArchiveList
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		t.Fatalf("réponse refusée: %v", err)
	}
	if len(list.Archives) != 1 {
		t.Fatalf("%d sauvegarde(s), attendue 1", len(list.Archives))
	}
	if list.Archives[0].Start.Year() != 2026 {
		t.Errorf("date de sauvegarde inattendue: %s", list.Archives[0].Start)
	}
	if list.Encryption.Mode != "repokey-blake2" {
		t.Errorf("mode = %q", list.Encryption.Mode)
	}
}
