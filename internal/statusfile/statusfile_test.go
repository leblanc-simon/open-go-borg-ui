package statusfile

import (
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/pkg/sftp"
)

var now = time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)

// valid est un état légitime.
func valid() Status {
	return Status{
		Hostname: "poste-marc", Started: now.Add(-2 * time.Hour), Finished: now.Add(-time.Hour),
		Result: ResultSuccess, Files: 42, OriginalSize: 1 << 30, DeduplicatedSize: 1 << 20,
		RepositorySize: 5 << 30, Encryption: "repokey-blake2", LastSuccess: now.Add(-time.Hour),
		NextRun: now.Add(10 * time.Hour),
	}
}

// encode sérialise un état en le modifiant au préalable.
func encode(t *testing.T, change func(*Status)) []byte {
	t.Helper()
	s := valid()
	if change != nil {
		change(&s)
	}
	data, err := Encode(s)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// TestRelectureValide vérifie l'aller-retour d'un état légitime.
func TestRelectureValide(t *testing.T) {
	got, err := Parse(encode(t, nil), "poste-marc.json", now)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := valid()
	want.Version = FormatVersion
	if !got.Finished.Equal(want.Finished) || got.Hostname != want.Hostname || got.RepositorySize != want.RepositorySize {
		t.Errorf("relu %+v", got)
	}
}

// TestFichiersPieges vérifie que chaque forme de fichier hostile est écartée
// (EF-86).
func TestFichiersPieges(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		file string
		want error
	}{
		{"trop volumineux", []byte(strings.Repeat(" ", MaxFileSize+1)), "poste-marc.json", ErrTooLarge},
		{"pas du JSON", []byte("<html>"), "poste-marc.json", ErrInvalid},
		{"fichier vide", nil, "poste-marc.json", ErrInvalid},
		{"se fait passer pour un autre", encode(t, nil), "poste-lucie.json", ErrNameMismatch},
		{"inversion de sens dans le nom", encode(t, func(s *Status) { s.Hostname = "poste‮txt.exe" }),
			FileName("poste‮txt.exe"), ErrInvalid},
		{"retour à la ligne dans le nom", encode(t, func(s *Status) { s.Hostname = "poste\nOK" }),
			FileName("poste\nOK"), ErrInvalid},
		{"nom démesuré", encode(t, func(s *Status) { s.Hostname = strings.Repeat("a", 65) }),
			FileName(strings.Repeat("a", 65)), ErrInvalid},
		{"toujours à jour", encode(t, func(s *Status) { s.Finished = now.AddDate(1, 0, 0) }), "poste-marc.json", ErrInvalid},
		{"fin avant début", encode(t, func(s *Status) { s.Started = s.Finished.Add(time.Hour) }), "poste-marc.json", ErrInvalid},
		{"résultat inconnu", encode(t, func(s *Status) { s.Result = "parfait" }), "poste-marc.json", ErrInvalid},
		{"taille négative", encode(t, func(s *Status) { s.Files = -1 }), "poste-marc.json", ErrInvalid},
		{"chiffrement exotique", encode(t, func(s *Status) { s.Encryption = "<script>" }), "poste-marc.json", ErrInvalid},
	}
	for _, c := range cases {
		if _, err := Parse(c.data, c.file, now); !errors.Is(err, c.want) {
			t.Errorf("%s: erreur %v, attendu %v", c.name, err, c.want)
		}
	}

	future := []byte(strings.Replace(string(encode(t, nil)), `"version": 1`, `"version": 2`, 1))
	if _, err := Parse(future, "poste-marc.json", now); !errors.Is(err, ErrUnsupported) {
		t.Errorf("version future: %v", err)
	}
}

// TestCleErreurFiltree vérifie qu'une clé de diagnostic hors de la racine
// error est effacée plutôt qu'affichée.
func TestCleErreurFiltree(t *testing.T) {
	for key, want := range map[string]string{
		"error.repository_locked": "error.repository_locked",
		"schedule.usage":          "",
		"error.":                  "",
		"error.a{{.X}}":           "",
	} {
		s, err := Parse(encode(t, func(s *Status) { s.Result = ResultError; s.ErrorKey = key }), "poste-marc.json", now)
		if err != nil {
			t.Fatalf("%s: %v", key, err)
		}
		if s.ErrorKey != want {
			t.Errorf("clé %q relue %q, attendu %q", key, s.ErrorKey, want)
		}
	}
}

// TestAppreciation vérifie l'indicateur de chaque poste, dont le seuil de
// 48 heures (EF-85).
func TestAppreciation(t *testing.T) {
	cases := []struct {
		result Result
		age    time.Duration
		health Health
		days   int
	}{
		{ResultSuccess, time.Hour, HealthOK, 0},
		{ResultWarning, time.Hour, HealthWarning, 0},
		{ResultError, time.Hour, HealthFailed, 0},
		{ResultCancelled, time.Hour, HealthFailed, 0},
		{ResultSuccess, 47 * time.Hour, HealthOK, 1},
		{ResultSuccess, 49 * time.Hour, HealthStale, 2},
		{ResultError, 5 * 24 * time.Hour, HealthStale, 5},
	}
	for _, c := range cases {
		health, days := Assess(Status{Result: c.result, Finished: now.Add(-c.age)}, now)
		if health != c.health || days != c.days {
			t.Errorf("%s depuis %v: %v, %d jours ; attendu %v, %d", c.result, c.age, health, days, c.health, c.days)
		}
	}
}

// TestNomCourt vérifie que le nom du poste est celui que Borg emploie.
func TestNomCourt(t *testing.T) {
	if got := Hostname("poste-marc.maison.lan"); got != "poste-marc" {
		t.Errorf("Hostname: %q", got)
	}
}

// memoryServer ouvre un client relié à un serveur SFTP en mémoire.
func memoryServer(t *testing.T) *sftp.Client {
	t.Helper()
	serverSide, clientSide := net.Pipe()
	server := sftp.NewRequestServer(serverSide, sftp.InMemHandler())
	go server.Serve()
	client, err := sftp.NewClientPipe(clientSide, clientSide)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close(); server.Close() })
	return client
}

// TestPublicationEtLecture vérifie le cycle complet sur un vrai serveur
// SFTP : dépôt, remplacement, relecture.
func TestPublicationEtLecture(t *testing.T) {
	client := memoryServer(t)

	if _, err := Read(client, DefaultDir, "poste-marc", now); !errors.Is(err, ErrNotPublished) {
		t.Fatalf("avant publication: %v, attendu ErrNotPublished", err)
	}
	if err := Publish(client, DefaultDir, valid()); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	again := valid()
	again.Files = 43
	if err := Publish(client, DefaultDir, again); err != nil {
		t.Fatalf("republication: %v", err)
	}

	got, err := Read(client, DefaultDir, "poste-marc", now)
	if err != nil || got.Files != 43 {
		t.Errorf("relu %+v, erreur %v", got, err)
	}
	// Le fichier temporaire ne subsiste pas.
	infos, _ := client.ReadDir(DefaultDir)
	if len(infos) != 1 {
		t.Errorf("fichiers restants: %d", len(infos))
	}
}

// TestLectureAlteree vérifie qu'un fichier altéré sur la destination est
// écarté plutôt que cru (EF-86).
func TestLectureAlteree(t *testing.T) {
	client := memoryServer(t)
	client.MkdirAll(DefaultDir)
	write := func(content string) {
		file, err := client.Create(DefaultDir + "/poste-marc.json")
		if err != nil {
			t.Fatal(err)
		}
		file.Write([]byte(content))
		file.Close()
	}

	lucie := valid()
	lucie.Hostname = "poste-lucie"
	write(string(encode(t, func(s *Status) { *s = lucie })))
	if _, err := Read(client, DefaultDir, "poste-marc", now); !errors.Is(err, ErrNameMismatch) {
		t.Errorf("état d'un autre poste: %v", err)
	}

	write(strings.Repeat(" ", MaxFileSize+10))
	if _, err := Read(client, DefaultDir, "poste-marc", now); !errors.Is(err, ErrTooLarge) {
		t.Errorf("fichier démesuré: %v", err)
	}
}
