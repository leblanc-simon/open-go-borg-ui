package borg

import (
	"encoding/json"
	"strings"
)

// EventKind distingue les natures d'événements émis par « --log-json ».
type EventKind int

const (
	// EventLog est un message de journal (niveau, identifiant, texte).
	EventLog EventKind = iota
	// EventArchiveProgress rapporte l'avancement d'une sauvegarde en cours :
	// volumes, nombre de fichiers et fichier traité.
	EventArchiveProgress
	// EventProgressPercent rapporte l'avancement d'une opération bornée.
	EventProgressPercent
	// EventProgressMessage rapporte une opération non bornée.
	EventProgressMessage
	// EventFileStatus rapporte le sort d'un fichier lors d'une sauvegarde.
	EventFileStatus
)

// Event est un événement de progression émis par Borg.
type Event struct {
	Kind EventKind

	// Operation identifie l'opération à laquelle l'événement se rattache.
	Operation int
	// MsgID est l'identifiant stable du message, utilisé pour traduire les
	// erreurs connues sans dépendre du texte anglais de Borg.
	MsgID string
	// Level est le niveau de journal (DEBUG, INFO, WARNING, ERROR).
	Level string
	// Message est le texte brut émis par Borg, en anglais.
	Message string
	// Finished indique la fin de l'opération.
	Finished bool

	// Current et Total bornent une progression, quand elle est connue.
	Current int64
	Total   int64

	// Path est le fichier en cours de traitement.
	Path string
	// Status est le sort d'un fichier : A ajouté, M modifié, U inchangé,
	// E erreur, x exclu.
	Status string

	// Files et les trois volumes décrivent l'avancement d'une sauvegarde.
	Files            int64
	OriginalSize     int64
	CompressedSize   int64
	DeduplicatedSize int64
}

// Percent retourne l'avancement en pourcentage, et false si l'opération n'est
// pas bornée.
func (e Event) Percent() (float64, bool) {
	if e.Total <= 0 {
		return 0, false
	}
	return float64(e.Current) / float64(e.Total) * 100, true
}

// Message est une entrée de journal conservée pour l'historique et le support.
type Message struct {
	Level string
	MsgID string
	Text  string
}

// logJSON reflète les champs des lignes émises par « borg --log-json ».
type logJSON struct {
	Type      string  `json:"type"`
	Operation int     `json:"operation"`
	MsgID     string  `json:"msgid"`
	LevelName string  `json:"levelname"`
	Message   string  `json:"message"`
	Finished  bool    `json:"finished"`
	Current   int64   `json:"current"`
	Total     int64   `json:"total"`
	Path      string  `json:"path"`
	Status    string  `json:"status"`
	NFiles    int64   `json:"nfiles"`
	Original  int64   `json:"original_size"`
	Compresse int64   `json:"compressed_size"`
	Deduped   int64   `json:"deduplicated_size"`
	Time      float64 `json:"time"`
}

// parseEvent interprète une ligne de stderr. Une ligne qui n'est pas du JSON
// reconnu est rendue comme un message de journal brut : Borg peut écrire
// directement sur stderr, et ces lignes portent souvent l'explication d'un
// échec.
func parseEvent(line []byte) (Event, bool) {
	trimmed := strings.TrimSpace(string(line))
	if trimmed == "" {
		return Event{}, false
	}
	if !strings.HasPrefix(trimmed, "{") {
		return Event{Kind: EventLog, Level: "RAW", Message: trimmed}, true
	}

	var raw logJSON
	if err := json.Unmarshal([]byte(trimmed), &raw); err != nil {
		return Event{Kind: EventLog, Level: "RAW", Message: trimmed}, true
	}

	event := Event{
		Operation: raw.Operation,
		MsgID:     raw.MsgID,
		Level:     raw.LevelName,
		Message:   raw.Message,
		Finished:  raw.Finished,
		Current:   raw.Current,
		Total:     raw.Total,
		Path:      raw.Path,
		Status:    raw.Status,
	}

	switch raw.Type {
	case "archive_progress":
		event.Kind = EventArchiveProgress
		event.Files = raw.NFiles
		event.OriginalSize = raw.Original
		event.CompressedSize = raw.Compresse
		event.DeduplicatedSize = raw.Deduped
	case "progress_percent":
		event.Kind = EventProgressPercent
	case "progress_message":
		event.Kind = EventProgressMessage
	case "file_status":
		event.Kind = EventFileStatus
	case "log_message":
		event.Kind = EventLog
	default:
		event.Kind = EventLog
	}
	return event, true
}
