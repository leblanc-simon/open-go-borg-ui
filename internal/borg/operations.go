package borg

import (
	"context"
	"encoding/json"
	"fmt"
)

// ArchivePattern est le motif de nom d'archive de l'application (EF-53). Borg
// substitue lui-même le nom du poste et l'horodatage.
const ArchivePattern = "{hostname}-{now:%Y-%m-%dT%H:%M:%S}"

// Encryption reflète le mode de chiffrement rapporté par le dépôt.
type Encryption struct {
	Mode string `json:"mode"`
}

// RepositoryInfo est la description d'un dépôt telle que « borg info » la
// rend.
type RepositoryInfo struct {
	Repository struct {
		ID           string `json:"id"`
		Location     string `json:"location"`
		LastModified string `json:"last_modified"`
	} `json:"repository"`
	Encryption Encryption `json:"encryption"`
	Cache      struct {
		Stats struct {
			TotalChunks     int64 `json:"total_chunks"`
			TotalSize       int64 `json:"total_size"`
			TotalCSize      int64 `json:"total_csize"`
			UniqueSize      int64 `json:"unique_size"`
			UniqueCSize     int64 `json:"unique_csize"`
			TotalUniqueChks int64 `json:"total_unique_chunks"`
		} `json:"stats"`
	} `json:"cache"`
}

// Archive est une sauvegarde présente dans le dépôt.
type Archive struct {
	Name  string    `json:"name"`
	ID    string    `json:"id"`
	Start Timestamp `json:"start"`
	Time  Timestamp `json:"time"`
}

// ArchiveList est la réponse de « borg list --json ».
type ArchiveList struct {
	Archives   []Archive  `json:"archives"`
	Encryption Encryption `json:"encryption"`
}

// CreateStats résume une sauvegarde terminée.
type CreateStats struct {
	Archive struct {
		Name     string  `json:"name"`
		Duration float64 `json:"duration"`
		Stats    struct {
			OriginalSize     int64 `json:"original_size"`
			CompressedSize   int64 `json:"compressed_size"`
			DeduplicatedSize int64 `json:"deduplicated_size"`
			NFiles           int64 `json:"nfiles"`
		} `json:"stats"`
	} `json:"archive"`
}

// InitOptions décrit la création d'un dépôt.
type InitOptions struct {
	Env Environment
	// Mode est le mode de chiffrement Borg. Il est figé pour la vie du dépôt :
	// en changer impose d'en créer un autre et de perdre l'historique (EF-32).
	Mode string
	// AppendOnly restreint le dépôt côté serveur (SEC-09).
	AppendOnly bool
}

// Init crée le dépôt.
func Init(ctx context.Context, runner Runner, opts InitOptions) (*Result, error) {
	flags := []string{"--encryption=" + opts.Mode}
	if opts.AppendOnly {
		flags = append(flags, "--append-only")
	}
	return runner.Run(ctx, Command{
		Name:    "init",
		Flags:   flags,
		Env:     opts.Env,
		LogJSON: true,
	})
}

// Info interroge le dépôt.
//
// Le mode de chiffrement d'un dépôt existant se lit ici et n'est jamais
// redemandé à l'utilisateur (EF-34).
func Info(ctx context.Context, runner Runner, env Environment) (*RepositoryInfo, *Result, error) {
	result, err := runner.Run(ctx, Command{
		Name:    "info",
		Flags:   []string{"--json"},
		Env:     env,
		LogJSON: true,
	})
	if err != nil {
		return nil, nil, err
	}
	if result.Status == StatusError {
		return nil, result, failure(result, "info")
	}

	var info RepositoryInfo
	if err := json.Unmarshal(result.Stdout, &info); err != nil {
		return nil, result, fmt.Errorf("borg: réponse de info illisible: %w", err)
	}
	return &info, result, nil
}

// List énumère les sauvegardes du dépôt.
func List(ctx context.Context, runner Runner, env Environment) (*ArchiveList, *Result, error) {
	result, err := runner.Run(ctx, Command{
		Name:    "list",
		Flags:   []string{"--json"},
		Env:     env,
		LogJSON: true,
	})
	if err != nil {
		return nil, nil, err
	}
	if result.Status == StatusError {
		return nil, result, failure(result, "list")
	}

	var list ArchiveList
	if err := json.Unmarshal(result.Stdout, &list); err != nil {
		return nil, result, fmt.Errorf("borg: réponse de list illisible: %w", err)
	}
	return &list, result, nil
}

// CreateOptions décrit une sauvegarde.
type CreateOptions struct {
	Env Environment
	// Archive est le nom de l'archive. Vide, le motif de l'application est
	// utilisé.
	Archive string
	// Sources sont les dossiers à sauvegarder, en chemins natifs.
	Sources []string
	// Excludes sont des motifs portables (**/node_modules), jamais des
	// chemins absolus, afin de rester valides sur les deux plateformes
	// (EF-43).
	Excludes []string
	// ExcludePaths sont des chemins natifs précis à écarter de cette
	// exécution seulement.
	ExcludePaths []string
	// ExcludeCaches fait respecter CACHEDIR.TAG (EF-44).
	ExcludeCaches bool
	// OneFileSystem empêche de franchir les points de montage (EF-47).
	OneFileSystem bool
	// Compression est la valeur Borg (lz4, zstd,3, zstd,9).
	Compression string
	// DryRun parcourt les sources sans rien écrire.
	DryRun bool
	// OnEvent reçoit la progression.
	OnEvent func(Event)
}

// Create exécute une sauvegarde.
//
// Les statistiques ne sont retournées que si Borg les a produites : une
// exécution terminée avec des avertissements (code 1) reste exploitable et
// n'est pas un échec (EF-56).
func Create(ctx context.Context, runner Runner, opts CreateOptions) (*CreateStats, *Result, error) {
	archive := opts.Archive
	if archive == "" {
		archive = ArchivePattern
	}

	flags := []string{"--stats", "--json", "--progress"}
	if opts.Compression != "" {
		flags = append(flags, "--compression="+opts.Compression)
	}
	if opts.ExcludeCaches {
		flags = append(flags, "--exclude-caches")
	}
	if opts.OneFileSystem {
		flags = append(flags, "--one-file-system")
	}
	if opts.DryRun {
		flags = append(flags, "--dry-run")
	}
	for _, pattern := range opts.Excludes {
		flags = append(flags, "--exclude", pattern)
	}

	result, err := runner.Run(ctx, Command{
		Name:         "create",
		Flags:        flags,
		Target:       "::" + archive,
		Sources:      opts.Sources,
		PathMode:     PathSources,
		ExcludePaths: opts.ExcludePaths,
		Env:          opts.Env,
		LogJSON:      true,
		OnEvent:      opts.OnEvent,
	})
	if err != nil {
		return nil, nil, err
	}
	if result.Status == StatusError {
		return nil, result, failure(result, "create")
	}

	var stats CreateStats
	if len(result.Stdout) > 0 {
		if err := json.Unmarshal(result.Stdout, &stats); err != nil {
			return nil, result, fmt.Errorf("borg: réponse de create illisible: %w", err)
		}
	}
	return &stats, result, nil
}

// ExtractOptions décrit une restauration.
type ExtractOptions struct {
	Env Environment
	// Archive est le nom de la sauvegarde à ouvrir.
	Archive string
	// Paths sont les chemins tels qu'ils figurent dans l'archive. Vide, toute
	// l'archive est extraite.
	Paths []string
	// Destination est le dossier où écrire, en chemin natif. La destination
	// par défaut de l'application est un dossier neuf, jamais l'emplacement
	// d'origine (EF-95).
	Destination string
	// OnEvent reçoit la progression.
	OnEvent func(Event)
}

// Extract restaure tout ou partie d'une archive.
func Extract(ctx context.Context, runner Runner, opts ExtractOptions) (*Result, error) {
	result, err := runner.Run(ctx, Command{
		Name:     "extract",
		Flags:    []string{"--progress"},
		Target:   "::" + opts.Archive,
		Sources:  opts.Paths,
		Dir:      opts.Destination,
		PathMode: PathExtract,
		Env:      opts.Env,
		LogJSON:  true,
		OnEvent:  opts.OnEvent,
	})
	if err != nil {
		return nil, err
	}
	if result.Status == StatusError {
		return result, failure(result, "extract")
	}
	return result, nil
}

// failure construit l'erreur d'une commande en échec, en y attachant le
// diagnostic connu quand il y en a un.
func failure(result *Result, name string) error {
	diagnosis, _ := result.Diagnose()
	return &CommandError{Name: name, ExitCode: result.ExitCode, Diagnosis: diagnosis, Messages: result.Messages}
}

// KeyExportPaper retourne la clé de secours de la destination, sous la forme
// imprimable de « borg key export --paper ». Avec la passphrase, c'est la
// seule façon de relire les sauvegardes si la destination perd sa copie de la
// clé : son export est obligatoire avant la première sauvegarde (EF-35).
func KeyExportPaper(ctx context.Context, runner Runner, env Environment) (string, *Result, error) {
	result, err := runner.Run(ctx, Command{
		Name:    "key",
		Flags:   []string{"export", "--paper"},
		Env:     env,
		LogJSON: true,
	})
	if err != nil {
		return "", nil, err
	}
	if result.Status == StatusError {
		return "", result, failure(result, "key export")
	}
	return string(result.Stdout), result, nil
}
