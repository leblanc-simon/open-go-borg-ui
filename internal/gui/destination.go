package gui

import (
	"context"
	"errors"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
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

// destinationScreen est l'écran Destination (EF-20 à EF-26, EF-33) : où
// partent les sauvegardes, la clé de ce poste, et le test de connexion.
type destinationScreen struct {
	u *ui

	key     *widget.Label
	test    *widget.Button
	status  *badge
	results *fyne.Container
	report  fyne.CanvasObject

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

	d.test = widget.NewButtonWithIcon(t("destination.test"), theme.MediaPlayIcon(), func() { d.runTest(false) })
	d.test.Importance = widget.HighImportance

	d.content = page(pageHeader(t("destination.title"), t("destination.subtitle"), d.test), column(
		d.summaryCard(profile),
		d.reportCard(),
		d.keyCard(),
	))
	d.loadKey(profile)
	return d
}

// summaryCard décrit la destination. Rien n'y est modifiable : le mode de
// chiffrement est figé à la création (EF-32, EF-33).
func (d *destinationScreen) summaryCard(profile *config.Profile) fyne.CanvasObject {
	t := d.u.t
	kind := t("destination.kind_hetzner")
	if profile.Destination.Kind == config.KindSSH {
		kind = t("destination.kind_ssh")
	}
	title := newText(kind, theme.SizeNameSubHeadingText, theme.ColorNameForeground, fyne.TextStyle{Bold: true})
	title.truncate = true
	address := newText("", sizeSmall, colorMuted, fyne.TextStyle{Monospace: true})
	address.truncate = true
	if url, err := profile.Destination.RepositoryURL(); err == nil {
		address.SetText(url)
	}

	encryption := newBadge(strings.ToUpper(t("encryption.none")), toneWarning)
	if profile.Encryption.Encrypted() {
		encryption.Set(strings.ToUpper(t("encryption.encrypted")), toneSuccess)
	}
	d.status = newBadge(strings.ToUpper(t("destination.status_unknown")), toneNeutral)

	fact := func(label, value string) fyne.CanvasObject {
		v := newText(value, theme.SizeNameText, theme.ColorNameForeground, fyne.TextStyle{Bold: true})
		v.truncate = true
		return container.NewVBox(caption(strings.ToUpper(label)), v)
	}
	facts := container.NewGridWithColumns(3)
	if profile.Destination.Kind == config.KindHetzner {
		facts.Add(fact(t("destination.account"), profile.Destination.User))
		facts.Add(fact(t("destination.name"), profile.Destination.Repo))
	}
	facts.Add(fact(t("destination.engine"), profile.Destination.RemotePath))

	encryptionNote := widget.NewLabel(t("destination.encryption_note"))
	encryptionNote.Wrapping = fyne.TextWrapWord
	encryptionNote.Importance = widget.LowImportance

	return card(container.NewVBox(
		container.NewBorder(nil, nil,
			container.NewCenter(newBubble(theme.StorageIcon(), toneInfo, 44)),
			container.NewVBox(container.NewHBox(d.status, encryption), layout.NewSpacer()),
			container.NewVBox(title, address)),
		spacer(4), widget.NewSeparator(), spacer(4),
		facts,
		encryptionNote,
	))
}

// reportCard accueille le compte rendu du test de connexion. Elle reste
// masquée tant qu'aucun test n'a été lancé.
func (d *destinationScreen) reportCard() fyne.CanvasObject {
	d.results = container.NewVBox()
	d.report = card(container.NewVBox(
		caption(strings.ToUpper(d.u.t("destination.report"))), d.results))
	d.report.Hide()
	return d.report
}

// keyCard montre la clé de ce poste et les deux façons de l'autoriser sur
// la destination : la déposer avec le mot de passe du compte, ou la coller
// soi-même dans la console Hetzner (EF-23, EF-24).
func (d *destinationScreen) keyCard() fyne.CanvasObject {
	t := d.u.t
	d.key = widget.NewLabel("")
	copyKey := widget.NewButtonWithIcon(t("destination.copy_key"), theme.ContentCopyIcon(), func() {
		d.u.app.Clipboard().SetContent(d.key.Text)
	})
	copyKey.Importance = widget.LowImportance
	steps := widget.NewLabel(t("destination.hetzner_steps"))
	steps.Wrapping = fyne.TextWrapWord
	steps.Importance = widget.LowImportance
	installer := d.u.keyInstaller(d.u.st.Profile, func() string { return "" }, func() { d.runTest(false) })

	return card(container.NewVBox(
		container.NewBorder(nil, nil, container.NewCenter(caption(strings.ToUpper(t("destination.key")))), copyKey),
		codeBlock(d.key),
		steps,
		widget.NewSeparator(),
		installer,
	))
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
	d.report.Show()
	d.content.Refresh()
	d.results.RemoveAll()
	d.results.Add(widget.NewLabel(t("destination.testing")))
	d.status.Set(strings.ToUpper(t("destination.status_testing")), toneInfo)

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
		reachable := len(lines) > 0
		for _, line := range lines {
			d.results.Add(d.u.renderLine(line))
			reachable = reachable && line.ok
		}
		if reachable {
			d.status.Set(strings.ToUpper(t("destination.status_ok")), toneSuccess)
		} else {
			d.status.Set(strings.ToUpper(t("destination.status_failed")), toneError)
		}
		d.content.Refresh()
		if unknownHost {
			d.u.confirmPin(fingerprint, func() { d.runTest(true) })
		}
	})
}

// diagnose exécute les étapes, puis la lecture de la destination par Borg,
// hors du fil de l'interface.
func (d *destinationScreen) diagnose(profile *config.Profile, pin bool) (lines []testLine, fingerprint string, unknownHost bool) {
	lines, fingerprint, unknownHost, ok := d.u.probeDestination(profile, pin)
	if !ok {
		return lines, fingerprint, unknownHost
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

// probeDestination déroule les étapes du diagnostic de connexion (EF-25) :
// nom, port, clé, moteur sur la destination. ok indique qu'elles ont toutes
// abouti ; sinon, la dernière ligne porte l'action corrective.
func (u *ui) probeDestination(profile *config.Profile, pin bool) (lines []testLine, fingerprint string, unknownHost, ok bool) {
	params, err := u.probeParams(profile, pin)
	if err != nil {
		return []testLine{{key: "error.unknown", detail: err.Error()}}, "", false, false
	}

	outcomes := probe.Run(context.Background(), params)
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
			return lines, fingerprint, unknownHost, false
		}
	}
	return lines, fingerprint, unknownHost, true
}

// probeParams décrit la connexion SSH à la destination du profil.
func (u *ui) probeParams(profile *config.Profile, pin bool) (probe.Params, error) {
	repository, err := profile.Destination.RepositoryURL()
	if err != nil {
		return probe.Params{}, err
	}
	host, port := station.HostPort(repository)
	keyPath, err := u.st.SSHKeyPath(profile)
	if err != nil {
		return probe.Params{}, err
	}
	knownHosts, err := config.KnownHostsPath()
	if err != nil {
		return probe.Params{}, err
	}
	return probe.Params{
		Host: host, Port: port, User: station.SSHUser(repository),
		KeyPath: keyPath, KnownHostsPath: knownHosts, AllowPinning: pin,
		RemotePath: profile.Destination.RemotePath, Timeout: probeTimeout,
	}, nil
}

// keyInstaller propose de déposer la clé du poste avec le mot de passe du
// compte, plutôt que de la coller dans la console Hetzner (EF-23, EF-24).
// Les clés déjà autorisées sont conservées. profile fournit le profil à
// l'instant du dépôt ; check retourne la clé d'un message bloquant, ou "" ;
// done est appelé une fois la clé en place, pour enchaîner sur le test.
func (u *ui) keyInstaller(profile func() (*config.Profile, error), check func() string, done func()) fyne.CanvasObject {
	t := u.t
	password := widget.NewPasswordEntry()
	password.SetPlaceHolder(t("destination.password"))
	result := container.NewVBox()

	var install *widget.Button
	var run func(pin bool)
	run = func(pin bool) {
		result.RemoveAll()
		if key := check(); key != "" {
			result.Add(u.renderLine(testLine{key: key}))
			return
		}
		install.Disable()
		result.Add(widget.NewLabel(t("destination.installing_key")))
		secret := password.Text
		var (
			outcome probe.InstallResult
			err     error
		)
		async(func() {
			var current *config.Profile
			var params probe.Params
			if current, err = profile(); err != nil {
				return
			}
			if params, err = u.probeParams(current, pin); err != nil {
				return
			}
			outcome, err = probe.InstallKey(context.Background(), params, secret)
		}, func() {
			install.Enable()
			result.RemoveAll()
			switch {
			case errors.Is(err, probe.ErrHostKeyUnknown):
				result.Add(u.renderLine(testLine{key: "destination.fix_host_unknown"}))
				u.confirmPin(outcome.Fingerprint, func() { run(true) })
			case errors.Is(err, probe.ErrHostKeyChanged):
				result.Add(u.renderLine(testLine{key: "destination.fix_host_changed", detail: err.Error()}))
			case errors.Is(err, probe.ErrPasswordRejected):
				result.Add(u.renderLine(testLine{key: "destination.fix_password"}))
			case err != nil:
				result.Add(u.renderLine(testLine{key: "destination.fix_install_key", detail: err.Error()}))
			default:
				// Le mot de passe ne sert qu'une fois : il ne reste pas à
				// l'écran.
				password.SetText("")
				key := "destination.key_installed"
				if !outcome.Added {
					key = "destination.key_already_installed"
				}
				result.Add(u.renderLine(testLine{ok: true, key: key}))
				done()
			}
		})
	}
	install = widget.NewButtonWithIcon(t("destination.install_key"), theme.UploadIcon(), func() { run(false) })
	install.Disable()
	password.OnChanged = func(value string) {
		if value == "" {
			install.Disable()
		} else {
			install.Enable()
		}
	}
	password.OnSubmitted = func(string) {
		if !install.Disabled() {
			run(false)
		}
	}

	return container.NewVBox(
		widget.NewLabelWithStyle(t("destination.install_title"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		u.wrapped(t("destination.install_text")),
		container.NewBorder(nil, nil, nil, install, password),
		result,
	)
}

// wrapped est un paragraphe qui revient à la ligne.
func (u *ui) wrapped(text string) *widget.Label {
	label := widget.NewLabel(text)
	label.Wrapping = fyne.TextWrapWord
	return label
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
func (u *ui) renderLine(line testLine) fyne.CanvasObject {
	icon := theme.ErrorIcon()
	if line.ok {
		icon = theme.ConfirmIcon()
	}
	text := u.t(line.key, line.data)
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
	return container.NewVBox(row, u.details(line.detail))
}

// confirmPin montre l'empreinte d'un serveur encore inconnu et ne
// l'enregistre qu'avec l'accord de l'utilisateur, en relançant alors le test
// (EF-26).
func (u *ui) confirmPin(fingerprint string, retry func()) {
	dialog.ShowConfirm(u.t("destination.pin_title"),
		u.t("destination.pin_question", map[string]any{"Fingerprint": fingerprint}),
		func(accepted bool) {
			if accepted {
				retry()
			}
		}, u.win)
}
