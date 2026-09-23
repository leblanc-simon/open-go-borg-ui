package schedule

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"leblanc.io/open-go-borg-ui/internal/config"
)

// recorder retient les commandes lancées. failing fait échouer celles dont la
// ligne contient le texte donné.
type recorder struct {
	calls   []string
	failing string
}

func (r *recorder) run(_ context.Context, name string, args ...string) ([]byte, error) {
	line := name + " " + strings.Join(args, " ")
	r.calls = append(r.calls, line)
	if r.failing != "" && strings.Contains(line, r.failing) {
		return nil, errors.New("échec simulé")
	}
	return nil, nil
}

// task est la tâche de test, avec des caractères qui éprouvent la citation.
var task = Task{
	Name:        "poste",
	Description: "Sauvegarde BorgUI — poste (100 %)",
	Executable:  "/home/marc/Applications/Borg UI/borgui",
	Args:        []string{"--config", "/home/marc/.config/borgui/$config.toml", "--run", "poste"},
}

// TestReglages vérifie la lecture de la configuration, cas valides et
// invalides.
func TestReglages(t *testing.T) {
	cases := []struct {
		in   config.Schedule
		want Plan
		ok   bool
	}{
		{config.Schedule{Kind: "manual"}, Plan{Frequency: Manual}, true},
		{config.Schedule{}, Plan{Frequency: Manual}, true},
		{config.Schedule{Kind: "daily", At: "22:30", CatchUpIfMissed: true},
			Plan{Frequency: Daily, Hour: 22, Minute: 30, CatchUp: true}, true},
		{config.Schedule{Kind: "weekly", At: "08:05", Day: "Friday"},
			Plan{Frequency: Weekly, Hour: 8, Minute: 5, Day: time.Friday}, true},
		{config.Schedule{Kind: "daily", At: "25:00"}, Plan{}, false},
		{config.Schedule{Kind: "daily"}, Plan{}, false},
		{config.Schedule{Kind: "weekly", At: "08:00", Day: "vendredi"}, Plan{}, false},
		{config.Schedule{Kind: "hourly", At: "08:00"}, Plan{}, false},
	}
	for _, c := range cases {
		got, err := FromConfig(c.in)
		if c.ok != (err == nil) {
			t.Errorf("%+v: erreur %v", c.in, err)
			continue
		}
		if c.ok && got != c.want {
			t.Errorf("%+v: %+v, attendu %+v", c.in, got, c.want)
		}
		if !c.ok && !errors.Is(err, ErrInvalid) {
			t.Errorf("%+v: erreur %v, attendu ErrInvalid", c.in, err)
		}
	}
}

// TestSystemd vérifie les unités écrites et les commandes lancées.
func TestSystemd(t *testing.T) {
	r := &recorder{}
	s := &Systemd{Dir: t.TempDir(), run: r.run}
	plan := Plan{Frequency: Weekly, Hour: 22, Minute: 0, Day: time.Monday, CatchUp: true}

	if err := s.Install(context.Background(), task, plan); err != nil {
		t.Fatalf("Install: %v", err)
	}

	service, _ := os.ReadFile(filepath.Join(s.Dir, "borgui-poste.service"))
	for _, want := range []string{
		`ExecStart="/home/marc/Applications/Borg UI/borgui" "--config" "/home/marc/.config/borgui/$$config.toml" "--run" "poste"`,
		"Description=Sauvegarde BorgUI — poste (100 %%)",
		"Type=oneshot",
	} {
		if !strings.Contains(string(service), want) {
			t.Errorf("service sans %q:\n%s", want, service)
		}
	}
	timer, _ := os.ReadFile(filepath.Join(s.Dir, "borgui-poste.timer"))
	for _, want := range []string{"OnCalendar=Mon *-*-* 22:00:00", "Persistent=true", "WantedBy=timers.target"} {
		if !strings.Contains(string(timer), want) {
			t.Errorf("timer sans %q:\n%s", want, timer)
		}
	}
	if got := strings.Join(r.calls, " | "); got !=
		"systemctl --user daemon-reload | systemctl --user enable --now borgui-poste.timer" {
		t.Errorf("commandes: %s", got)
	}

	if installed, _ := s.Installed(context.Background(), "poste"); !installed {
		t.Error("la tâche devrait être vue comme installée")
	}
	if err := s.Remove(context.Background(), "poste"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if installed, _ := s.Installed(context.Background(), "poste"); installed {
		t.Error("la tâche devrait être retirée")
	}
	if entries, _ := os.ReadDir(s.Dir); len(entries) != 0 {
		t.Errorf("unités restantes: %v", entries)
	}
}

// TestSystemdRetraitAbsent vérifie que retirer une tâche jamais installée ne
// lance rien et ne signale rien.
func TestSystemdRetraitAbsent(t *testing.T) {
	r := &recorder{}
	s := &Systemd{Dir: t.TempDir(), run: r.run}
	if err := s.Remove(context.Background(), "poste"); err != nil || len(r.calls) != 0 {
		t.Errorf("erreur %v, commandes %v", err, r.calls)
	}
}

// TestManuelNonInstallable vérifie qu'une planification manuelle n'est pas
// installée par erreur.
func TestManuelNonInstallable(t *testing.T) {
	s := &Systemd{Dir: t.TempDir(), run: (&recorder{}).run}
	if err := s.Install(context.Background(), task, Plan{Frequency: Manual}); !errors.Is(err, ErrInvalid) {
		t.Errorf("erreur %v, attendu ErrInvalid", err)
	}
}

// decodeUTF16 relit le XML produit pour Windows.
func decodeUTF16(t *testing.T, data []byte) string {
	t.Helper()
	if len(data) < 2 || data[0] != 0xFF || data[1] != 0xFE {
		t.Fatal("marque d'ordre des octets UTF-16LE absente")
	}
	units := make([]uint16, 0, len(data)/2)
	for i := 2; i+1 < len(data); i += 2 {
		units = append(units, uint16(data[i])|uint16(data[i+1])<<8)
	}
	return string(utf16.Decode(units))
}

// TestDefinitionWindows vérifie la tâche Windows : rattrapage, absence
// d'élévation, pas de limite de durée, arguments cités et XML échappé.
func TestDefinitionWindows(t *testing.T) {
	win := Task{
		Name:        "poste",
		Description: "Sauvegarde <BorgUI> & co",
		Executable:  `C:\Users\marc\AppData\Local\Programs\Borg UI\borgui.exe`,
		Args:        []string{"--config", `C:\Users\marc\Mes documents\config.toml`, "--run", `poste "principal"`},
	}
	plan := Plan{Frequency: Weekly, Hour: 7, Minute: 45, Day: time.Sunday, CatchUp: true}
	text := decodeUTF16(t, taskXML(win, plan))

	for _, want := range []string{
		`encoding="UTF-16"`,
		"<StartBoundary>2026-01-01T07:45:00</StartBoundary>",
		"<DaysOfWeek><Sunday /></DaysOfWeek>",
		"<StartWhenAvailable>true</StartWhenAvailable>",
		"<LogonType>InteractiveToken</LogonType>",
		"<RunLevel>LeastPrivilege</RunLevel>",
		"<ExecutionTimeLimit>PT0S</ExecutionTimeLimit>",
		"<Description>Sauvegarde &lt;BorgUI&gt; &amp; co</Description>",
		`<Command>C:\Users\marc\AppData\Local\Programs\Borg UI\borgui.exe</Command>`,
		`<Arguments>--config &#34;C:\Users\marc\Mes documents\config.toml&#34; --run &#34;poste \&#34;principal\&#34;&#34;</Arguments>`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("définition sans %q:\n%s", want, text)
		}
	}
}

// TestPlanificateurWindows vérifie les commandes schtasks.
func TestPlanificateurWindows(t *testing.T) {
	r := &recorder{}
	s := &TaskScheduler{run: r.run}
	if err := s.Install(context.Background(), task, Plan{Frequency: Daily, Hour: 22}); err != nil {
		t.Fatal(err)
	}
	if len(r.calls) != 1 || !strings.HasPrefix(r.calls[0], "schtasks /Create /TN BorgUI-poste /XML ") ||
		!strings.HasSuffix(r.calls[0], " /F") {
		t.Errorf("commandes: %v", r.calls)
	}

	// Une tâche absente fait échouer /Query : Remove ne doit alors rien
	// supprimer ni signaler.
	r = &recorder{failing: "/Query"}
	s = &TaskScheduler{run: r.run}
	if err := s.Remove(context.Background(), "poste"); err != nil || len(r.calls) != 1 {
		t.Errorf("erreur %v, commandes %v", err, r.calls)
	}
}

// TestCitationWindows fixe les règles de CommandLineToArgvW.
func TestCitationWindows(t *testing.T) {
	cases := map[string]string{
		"poste":           "poste",
		"":                `""`,
		`a b`:             `"a b"`,
		`C:\dossier\`:     `C:\dossier\`,
		`C:\mon dossier\`: `"C:\mon dossier\\"`,
		`dit "oui"`:       `"dit \"oui\""`,
		`fin\"`:           `"fin\\\""`,
	}
	for in, want := range cases {
		if got := windowsQuote(in); got != want {
			t.Errorf("windowsQuote(%q) = %s, attendu %s", in, got, want)
		}
	}
}

// TestProchaineExecution vérifie le calcul de la prochaine exécution, y
// compris l'heure du jour déjà passée et le changement d'heure.
func TestProchaineExecution(t *testing.T) {
	paris, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Skip("base des fuseaux indisponible")
	}
	at := func(y int, m time.Month, d, h, min int) time.Time { return time.Date(y, m, d, h, min, 0, 0, paris) }

	cases := []struct {
		name string
		plan Plan
		now  time.Time
		want time.Time
	}{
		{"plus tard dans la journée", Plan{Frequency: Daily, Hour: 22}, at(2026, 9, 24, 10, 0), at(2026, 9, 24, 22, 0)},
		{"heure passée : lendemain", Plan{Frequency: Daily, Hour: 22}, at(2026, 9, 24, 22, 0), at(2026, 9, 25, 22, 0)},
		{"jeudi → vendredi", Plan{Frequency: Weekly, Hour: 21, Minute: 30, Day: time.Friday}, at(2026, 9, 24, 12, 0), at(2026, 9, 25, 21, 30)},
		{"vendredi passé → suivant", Plan{Frequency: Weekly, Hour: 21, Minute: 30, Day: time.Friday}, at(2026, 9, 25, 22, 0), at(2026, 10, 2, 21, 30)},
		{"passage à l'heure d'hiver", Plan{Frequency: Daily, Hour: 3}, at(2026, 10, 24, 12, 0), at(2026, 10, 25, 3, 0)},
		{"manuelle", Plan{Frequency: Manual}, at(2026, 9, 24, 12, 0), time.Time{}},
	}
	for _, c := range cases {
		if got := Next(c.plan, c.now); !got.Equal(c.want) {
			t.Errorf("%s: %v, attendu %v", c.name, got, c.want)
		}
	}
}
