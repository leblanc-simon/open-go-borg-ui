package history

import (
	"context"
	"testing"
	"time"
)

// TestVerificationsDeRestauration vérifie la consignation des
// vérifications et la lecture de la plus récente, profil par profil.
func TestVerificationsDeRestauration(t *testing.T) {
	store := openCatalog(t)
	ctx := context.Background()
	if _, ok, err := store.LastRestoreCheck(ctx, "poste"); ok || err != nil {
		t.Fatalf("aucune vérification attendue: %v, %v", ok, err)
	}
	earlier := time.Date(2026, 8, 24, 22, 0, 0, 0, time.Local)
	later := earlier.AddDate(0, 1, 0)
	store.AddRestoreCheck(ctx, RestoreCheck{Profile: "poste", Checked: earlier, Outcome: OutcomeFailed, Detail: "x"})
	store.AddRestoreCheck(ctx, RestoreCheck{Profile: "poste", Checked: later, Archive: "poste-1",
		Path: "home/marc/a.txt", Native: "/home/marc/a.txt", Outcome: OutcomeIdentical})
	store.AddRestoreCheck(ctx, RestoreCheck{Profile: "autre", Checked: later.Add(time.Hour), Outcome: OutcomeFailed})

	last, ok, err := store.LastRestoreCheck(ctx, "poste")
	if err != nil || !ok || last.Outcome != OutcomeIdentical || !last.Checked.Equal(later) || last.Native != "/home/marc/a.txt" {
		t.Errorf("dernière vérification: %+v, %v, %v", last, ok, err)
	}
}

// TestTirageAuHasard vérifie que seul un fichier non vide et assez petit
// peut être tiré.
func TestTirageAuHasard(t *testing.T) {
	store := openCatalog(t)
	ctx := context.Background()
	store.SaveCatalog(ctx, "a1", []Entry{
		{Path: "home/marc/vide.txt", Size: 0},
		{Path: "home/marc/énorme.iso", Size: 1 << 40},
		{Path: "home/marc/lettre.odt", Size: 2048},
	}, time.Now())

	for i := 0; i < 10; i++ {
		entry, ok, err := store.RandomFile(ctx, "a1", 1<<20)
		if err != nil || !ok || entry.Path != "home/marc/lettre.odt" {
			t.Fatalf("tirage: %+v, %v, %v", entry, ok, err)
		}
	}
	if _, ok, _ := store.RandomFile(ctx, "a1", 1024); ok {
		t.Error("aucun fichier assez petit : rien ne doit être tiré")
	}
}

// TestControlesDeLaDestination vérifie la consignation des contrôles et la
// lecture du plus récent.
func TestControlesDeLaDestination(t *testing.T) {
	store := openCatalog(t)
	ctx := context.Background()
	if _, ok, err := store.LastRepositoryCheck(ctx, "poste"); ok || err != nil {
		t.Fatalf("aucun contrôle attendu: %v, %v", ok, err)
	}
	earlier := time.Date(2026, 8, 26, 22, 0, 0, 0, time.Local)
	store.AddRepositoryCheck(ctx, RepositoryCheck{Profile: "poste", Checked: earlier, Healthy: true})
	store.AddRepositoryCheck(ctx, RepositoryCheck{Profile: "poste", Checked: earlier.AddDate(0, 1, 0),
		Healthy: false, Duration: 90 * time.Second, Detail: "segment 12 corrompu"})

	last, ok, err := store.LastRepositoryCheck(ctx, "poste")
	if err != nil || !ok || last.Healthy || last.Duration != 90*time.Second || last.Detail != "segment 12 corrompu" {
		t.Errorf("dernier contrôle: %+v, %v, %v", last, ok, err)
	}
}
