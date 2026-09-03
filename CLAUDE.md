# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## État du dépôt

Le dépôt ne contient **que des spécifications** — aucun code, pas de `go.mod`, pas de dépôt Git initialisé. Le projet (nom de travail : **BorgUI**) est une application desktop Go/Fyne de configuration et de supervision de sauvegardes BorgBackup vers une Hetzner Storage Box.

Les trois documents de `specs/` font autorité et se lisent dans cet ordre :

| Fichier | Rôle |
|---|---|
| `specs/cahier-des-charges.md` | Contrat : exigences numérotées (EF-xx fonctionnelles, EI-xx interface, ENF-xx non fonctionnelles, AR-xx architecture, SEC-xx sécurité, TR-xx recette), jalons, livrables |
| `specs/addendum-decisions.md` | Décisions arrêtées et pièges d'implémentation détaillés |
| `specs/rapport-gui-borgbackup.md` | Étude préalable : justification des choix, comparatifs, état de l'art Borg/Hetzner |

Toute décision d'implémentation doit se rattacher à une référence de ces documents. Les exigences y sont priorisées **O** (obligatoire v1) / **I** (important) / **S** (souhaité).

## Décisions techniques figées

Ces choix ne sont pas des arbitrages ouverts (`cahier-des-charges.md` §3) :

- Go 1.22+, Fyne 2.7.x, BorgBackup **1.4.x** non modifié (jamais 2.x)
- **Un seul exécutable** par plateforme, servant à la fois d'interface et d'agent planifié
- Configuration en TOML éditable à la main, historique et cache en SQLite (`modernc.org/sqlite`, pur Go)
- Windows : runtime Borg **Cygwin téléchargé au premier lancement** (jamais embarqué), vérifié par SHA-256 compilée dans le binaire
- Restauration sur `borg list --json` + `borg extract`, **jamais** `borg mount` (pas de FUSE sous Windows → parité impossible)
- Aucun droit administrateur, aucun démon résident
- Un dépôt Borg et un sous-compte Hetzner **par poste**, jamais mutualisés

Bibliothèques prévues : `golang.org/x/crypto/ssh` + `github.com/pkg/sftp` (diagnostic et tableau de bord, sans binaire externe), `github.com/zalando/go-keyring` (passphrase), `fyne.io/systray` (v0.3).

## Architecture

```
Interface (Fyne)  — n'appelle JAMAIS Borg directement
        │
Cœur applicatif
   ├─ ConfigStore    (TOML)
   ├─ SecretStore    (trousseau OS, optionnel selon le mode de chiffrement)
   ├─ HistoryStore   (SQLite)
   ├─ RuntimeManager (téléchargement, vérification, versions)
   ├─ Scheduler      (schtasks / systemd --user)
   ├─ StatusPublisher(SFTP)
   └─ BorgRunner     ← interface
         ├─ NativeRunner   (Linux)
         └─ CygwinRunner   (Windows)
        │
BorgBackup 1.4  ──ssh port 23──►  Hetzner Storage Box
```

Invariants (AR-01 à AR-06) :

- **Toute** exécution de Borg passe par l'interface `BorgRunner`. Aucun `os/exec` de Borg ailleurs.
- La **traduction des chemins est de la responsabilité exclusive du Runner** — ni l'interface ni la configuration ne connaissent `/cygdrive`.
- Modes du binaire : interface (défaut), `--run <profil>`, `--check <profil>`, `--install-schedule <profil>`, `--print-passphrase <profil>`. La tâche planifiée invoque `--run`.
- Configuration : `~/.config/borgui/config.toml` (Linux), `%APPDATA%\borgui\config.toml` (Windows). Runtime Windows : `%LOCALAPPDATA%\borgui\runtime\<version>\`.

## Pièges qui coûtent cher

- **`BORG_UNKNOWN_UNENCRYPTED_REPO_ACCESS_IS_OK=yes` est obligatoire en mode non chiffré.** Sans lui, Borg pose une question interactive et toute sauvegarde planifiée reste bloquée indéfiniment.
- **Le mode de chiffrement est irréversible** (figé à `borg init`). Il est demandé une seule fois dans l'assistant, jamais modifiable ailleurs, et pour un dépôt existant il est **lu** via `borg info --json`, jamais redemandé. Deux modes exposés seulement : `repokey-blake2` et `none`.
- **Convention de chemins Windows, à ne jamais changer** : `borg create` s'exécute avec le répertoire courant sur `/cygdrive` et reçoit des chemins relatifs préfixés de la lettre de lecteur (`c/Users/marc/Documents`). L'archive contient donc `c/…` sans préfixe `cygdrive`. Restauration : `cd C:\` puis `borg extract … c --strip-components 1`.
- **Code de retour Borg `1` = avertissement**, pas échec : état orange « terminé avec avertissements », la sauvegarde est exploitable. `0` succès, `2` erreur.
- La passphrase passe par `BORG_PASSCOMMAND` invoquant l'application elle-même, **jamais** par `BORG_PASSPHRASE` ni par la ligne de commande.
- `--remote-path` (Hetzner : `borg-1.4`) sur **toutes** les commandes ; dépôt de la forme `ssh://uXXXXXX@uXXXXXX.your-storagebox.de:23/./nom` — le `/./` est significatif.
- Exclusions stockées comme motifs portables (`**/node_modules`), jamais comme chemins absolus.
- Sous Windows, exclure par défaut les fichiers en espace réservé cloud (`FILE_ATTRIBUTE_RECALL_ON_DATA_ACCESS`) : les inclure déclenche l'hydratation complète de OneDrive/Dropbox.
- Toute modification de rétention affiche un `borg prune --list --dry-run` avant confirmation.
- Le cache SQLite des listes d'archives n'est **jamais** invalidé : les archives sont immuables.

## Interface

- Quatre écrans au maximum — État, Sauvegarde, Destination, Réglages — plus une fenêtre dédiée à la restauration.
- **Le vocabulaire de Borg n'apparaît jamais à l'écran** : ni « repository », ni « archive », ni « prune », ni « chunk ». On dit destination, sauvegarde, conservation. Vérifié par la recette TR-42.
- Aucun appel à Borg ne bloque l'interface : asynchrone, avec progression (`--log-json`) et annulation quand c'est possible.
- Chaque erreur Borg connue est traduite en français avec une action proposée ; le journal brut reste sous un dépliant « Détails ».
- L'application est pleinement utilisable sans jamais ouvrir l'écran Réglages.

## Feuille de route

La **v0.1 est réalisée en premier, sans aucune interface** : ligne de commande validant téléchargement du runtime, connexion à une vraie Storage Box, `init` dans les deux modes, `create` sur données réelles, `list`, puis **restauration de l'archive Windows par le `borg` officiel d'une machine Linux**. Cette étape porte le risque principal du projet ; si elle échoue, l'architecture est à revoir.

Puis v0.2 (MVP : écrans État/Sauvegarde/Destination, assistant, voie hors ligne), v0.3 (planification, statuts SFTP multi-postes), v1.0 (restauration guidée, test de restauration mensuel, traduction des erreurs).

## Développement

Le module n'est pas encore initialisé. À la création :

```bash
go mod init <module>
go build ./...
go test ./...
go test ./chemin/du/paquet -run TestNom -v   # un test isolé
go vet ./...
```

**Fyne dépend de CGO** : pas de compilation croisée. La CI doit avoir deux exécuteurs, `windows-latest` et `ubuntu-latest` (LI-04). Les couches non graphiques (`BorgRunner`, stores, scheduler) doivent rester testables sans Fyne.

Contraintes vérifiables : exécutable ≤ 30 Mo, démarrage < 2 s, aucune opération bloquante > 100 ms, mémoire au repos < 150 Mo.

## Git

Les messages de commit ne mentionnent **jamais** Claude : pas de ligne `Co-Authored-By: Claude`, pas de mention « Generated with Claude Code », aucune signature d'agent. Idem pour les descriptions de pull request.
