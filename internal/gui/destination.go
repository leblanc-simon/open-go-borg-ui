package gui

import (
	"context"
	"errors"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"leblanc.io/open-go-borg-ui/internal/borg"
	"leblanc.io/open-go-borg-ui/internal/config"
	"leblanc.io/open-go-borg-ui/internal/format"
	"leblanc.io/open-go-borg-ui/internal/probe"
	"leblanc.io/open-go-borg-ui/internal/station"
)

// probeTimeout borne chaque étape du test de connexion.
const probeTimeout = 20 * time.Second

// destinationScreen est l'écran Destination (EF-20 à EF-26, EF-33).
type destinationScreen struct {
	u *ui

	key     *widget.Label
	test    *widget.Button
	results *fyne.Container

	content fyne.CanvasObject
}

func newDestinationScreen(u *ui) *destinationScreen {
	d := &destinationScreen{u: u}
	t := u.t

	profile, err := u.st.Profile()
	if err != nil {
		d.content = widget.NewLabel(err.Error())
		return d
	}

	kind := t("destination.kind_hetzner")
	if profile.Destination.Kind == config.KindSSH {
		kind = t("destination.kind_ssh")
	}
	encryption := t("encryption.none")
	if profile.Encryption.Encrypted() {
		encryption = t("encryption.encrypted")
	}
	form := widget.NewForm(
		widget.NewFormItem(t("destination.kind"), widget.NewLabel(kind)),
		widget.NewFormItem(t("destination.account"), widget.NewLabel(profile.Destination.User)),
		widget.NewFormItem(t("destination.name"), widget.NewLabel(profile.Destination.Repo)),
		widget.NewFormItem(t("destination.engine"), widget.NewLabel(profile.Destination.RemotePath)),
		// Le mode est affiché, jamais modifiable ici : il est figé à la
		// création de la destination (EF-32, EF-33).
		widget.NewFormItem(t("destination.encryption"), widget.NewLabel(t("destination.encryption_fixed", map[string]any{"Mode": encryption}))),
	)

	d.key = widget.NewLabel("")
	d.key.Wrapping = fyne.TextWrapBreak
	d.key.TextStyle = fyne.TextStyle{Monospace: true}
	copyKey := widget.NewButtonWithIcon(t("destination.copy_key"), theme.ContentCopyIcon(), func() {
		u.app.Clipboard().SetContent(d.key.Text)
	})
	steps := widget.NewLabel(t("destination.hetzner_steps"))
	steps.Wrapping = fyne.TextWrapWord

	d.test = widget.NewButtonWithIcon(t("destination.test"), theme.MediaPlayIcon(), func() { d.runTest(false) })
	d.results = container.NewVBox()

	d.content = container.NewVScroll(container.NewPadded(container.NewVBox(
		form,
		widget.NewSeparator(),
		widget.NewLabelWithStyle(t("destination.key"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		d.key, container.NewHBox(copyKey), steps,
		widget.NewSeparator(),
		container.NewHBox(d.test), d.results,
	)))
	d.loadKey(profile)
	return d
}

// loadKey affiche la clé publique, en la créant au besoin (EF-23).
func (d *destinationScreen) loadKey(profile *config.Profile) {
	var key string
	var keyErr error
	async(func() {
		path, err := d.u.st.SSHKeyPath(profile)
		if err != nil {
			keyErr = err
			return
		}
		if _, err := probe.EnsureKey(path); err != nil {
			keyErr = err
			return
		}
		key, keyErr = probe.PublicKey(path)
	}, func() {
		if keyErr != nil {
			d.key.SetText(keyErr.Error())
			return
		}
		d.key.SetText(key)
	})
}

// testLine est une ligne du compte rendu de test.
type testLine struct {
	ok     bool
	key    string
	data   map[string]any
	detail string
}

// runTest déroule le diagnostic, puis la lecture de la destination par Borg.
// pin autorise l'enregistrement de l'empreinte du serveur, que l'utilisateur
// vient d'accepter (EF-26).
func (d *destinationScreen) runTest(pin bool) {
	t := d.u.t
	d.test.Disable()
	d.results.RemoveAll()
	d.results.Add(widget.NewLabel(t("destination.testing")))

	var (
		lines       []testLine
		fingerprint string
		unknownHost bool
	)
	async(func() {
		profile, err := d.u.st.Profile()
		if err != nil {
			lines = append(lines, testLine{key: "error.unknown", detail: err.Error()})
			return
		}
		lines, fingerprint, unknownHost = d.diagnose(profile, pin)
	}, func() {
		d.test.Enable()
		d.results.RemoveAll()
		for _, line := range lines {
			d.results.Add(d.renderLine(line))
		}
		if unknownHost {
			d.confirmPin(fingerprint)
		}
	})
}

// diagnose exécute les étapes, hors du fil de l'interface.
func (d *destinationScreen) diagnose(profile *config.Profile, pin bool) (lines []testLine, fingerprint string, unknownHost bool) {
	repository, err := profile.Destination.RepositoryURL()
	if err != nil {
		return []testLine{{key: "error.unknown", detail: err.Error()}}, "", false
	}
	host, port := station.HostPort(repository)
	user := station.SSHUser(repository)
	keyPath, err := d.u.st.SSHKeyPath(profile)
	if err != nil {
		return []testLine{{key: "error.unknown", detail: err.Error()}}, "", false
	}
	knownHosts, err := config.KnownHostsPath()
	if err != nil {
		return []testLine{{key: "error.unknown", detail: err.Error()}}, "", false
	}

	outcomes := probe.Run(context.Background(), probe.Params{
		Host: host, Port: port, User: user,
		KeyPath: keyPath, KnownHostsPath: knownHosts, AllowPinning: pin,
		RemotePath: profile.Destination.RemotePath, Timeout: probeTimeout,
	})
	for _, outcome := range outcomes {
		line := testLine{ok: outcome.OK, key: outcome.Step.TranslationKey()}
		if outcome.OK {
			line.detail = outcome.Detail
		} else {
			if outcome.Step == probe.StepAuthenticate {
				fingerprint = outcome.Detail
			}
			if errors.Is(outcome.Err, probe.ErrHostKeyUnknown) {
				unknownHost = true
			}
			if outcome.Err != nil {
				line.detail = outcome.Err.Error()
			}
		}
		lines = append(lines, line)
		if !outcome.OK {
			lines = append(lines, testLine{key: fixKey(outcome.Step, outcome.Err)})
			return lines, fingerprint, unknownHost
		}
	}

	// Dernière étape : la destination elle-même, lue par Borg avec les
	// réglages du profil.
	runner, err := d.u.st.Runner()
	if err != nil {
		return append(lines, testLine{key: errorKey(err), detail: err.Error()}), "", false
	}
	env, err := d.u.st.Environment(profile)
	if err != nil {
		return append(lines, testLine{key: "error.unknown", detail: err.Error()}), "", false
	}
	info, result, err := borg.Info(context.Background(), runner, env)
	switch {
	case err == nil:
		lines = append(lines, testLine{ok: true, key: "destination.readable", data: map[string]any{
			"Size": format.Size(info.Cache.Stats.UniqueCSize),
		}})
	case result != nil && isMissing(result):
		// Une destination qui n'a pas encore servi n'est pas une anomalie.
		lines = append(lines, testLine{ok: true, key: "destination.empty"})
	default:
		lines = append(lines, testLine{key: errorKey(err), detail: err.Error()})
	}
	return lines, "", false
}

// isMissing indique une destination encore vide.
func isMissing(result *borg.Result) bool {
	diagnosis, failed := result.Diagnose()
	return failed && diagnosis == borg.FailureRepositoryMissing
}

// fixKey retourne l'action corrective d'une étape en échec.
func fixKey(step probe.Step, err error) string {
	switch {
	case errors.Is(err, probe.ErrHostKeyChanged):
		return "destination.fix_host_changed"
	case errors.Is(err, probe.ErrHostKeyUnknown):
		return "destination.fix_host_unknown"
	}
	switch step {
	case probe.StepResolve:
		return "connection.fix_resolve"
	case probe.StepReach:
		return "connection.fix_reach"
	case probe.StepAuthenticate:
		return "destination.fix_authenticate"
	default:
		return "destination.fix_engine"
	}
}

// renderLine affiche une ligne du compte rendu.
func (d *destinationScreen) renderLine(line testLine) fyne.CanvasObject {
	icon := theme.ErrorIcon()
	if line.ok {
		icon = theme.ConfirmIcon()
	}
	text := d.u.t(line.key, line.data)
	if line.ok && line.detail != "" {
		text += " — " + line.detail
		line.detail = ""
	}
	label := widget.NewLabel(text)
	label.Wrapping = fyne.TextWrapWord
	row := container.NewBorder(nil, nil, widget.NewIcon(icon), nil, label)
	if line.detail == "" {
		return row
	}
	return container.NewVBox(row, d.u.details(line.detail))
}

// confirmPin montre l'empreinte d'un serveur encore inconnu et ne
// l'enregistre qu'avec l'accord de l'utilisateur (EF-26).
func (d *destinationScreen) confirmPin(fingerprint string) {
	t := d.u.t
	dialog.ShowConfirm(t("destination.pin_title"),
		t("destination.pin_question", map[string]any{"Fingerprint": fingerprint}),
		func(accepted bool) {
			if accepted {
				d.runTest(true)
			}
		}, d.u.win)
}
