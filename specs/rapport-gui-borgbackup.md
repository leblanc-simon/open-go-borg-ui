# Rapport technique — Application desktop de configuration BorgBackup (Windows + Linux)

Date : 3 septembre 2026
Objet : choix de langage, de bibliothèque UI, et architecture pour une GUI simple pilotant BorgBackup vers une Storage Box Hetzner.

---

## 1. Résumé exécutif

**Le point bloquant n'est pas l'UI, c'est Borg sur Windows.** Borg est officiellement supporté sur Linux, macOS et les BSD uniquement. Sur Windows, il n'existe que des voies expérimentales (WSL, Cygwin) ou un fork non officiel. Toute la conception du projet dépend du choix fait ici, donc c'est la première décision à prendre — avant d'écrire une ligne de GUI.

**Recommandations principales :**

| Sujet | Recommandation |
|---|---|
| Version de Borg ciblée | **1.4.x** (1.4.5 stable). Borg 2.0 est encore en beta et Hetzner ne propose que 1.2 et 1.4 côté serveur. |
| Moteur sous Linux | `borg` du paquet distribution, ou binaire officiel *fat binary* embarqué |
| Moteur sous Windows | **Embarquer un runtime Borg** (bundle Cygwin type `borg4win`, ou le fork natif `borg-windows`) plutôt que dépendre de WSL2 |
| Langage + UI | **Go + Fyne** (binaire unique, `exec` + JSON natifs, embarquement du runtime Borg via `go:embed`) |
| Alternative sérieuse | **C# / .NET 9 + Avalonia** si vous voulez une UI plus riche et une chaîne de build sans Docker |
| À ne pas faire | Electron / Tauri : dépendance webview (WebKitGTK) pénible à packager sous Linux, pour une UI de 4 écrans |
| Avant de coder | Regarder Vorta, Pika Backup, WinBorg et borgmatic — il y a peut-être 80 % du travail déjà fait |

**Objectif « un seul binaire » : atteignable sous Linux et sous Windows**, à condition d'embarquer le runtime Borg dans le binaire et de l'extraire au premier lancement dans le répertoire de données de l'application.

---

## 2. Faits vérifiés (état de l'art, septembre 2026)

### 2.1 BorgBackup

- **1.4.5** est la série stable courante (sortie le 19/07/2026). **1.2.9** est l'oldstable. **2.0 est toujours en beta** (2.0.0b24), avec un avertissement explicite « ne pas utiliser sur des dépôts de production ». → **Ciblez 1.4.**
- Plateformes officielles : Linux, macOS, FreeBSD, NetBSD, OpenBSD. **Windows : « expérimental » via Cygwin ou WSL.**
- Format de dépôt 1.2 et 1.4 compatibles entre eux ; 2.0 casse le format (migration par `borg transfer`).

### 2.2 Hetzner Storage Box

Points concrets à intégrer dans le code :

- Accès Borg via le **service SSH étendu sur le port 23**. Il faut activer dans la console Hetzner : **SSH support** et **External reachability**.
- Forme du dépôt : `ssh://uXXXXX@uXXXXX.your-storagebox.de:23/./nom-du-depot` — le `/./` (chemin relatif) est important, seul `/home/` est inscriptible.
- **Deux versions de Borg sont installées côté serveur : 1.2 et 1.4.** Hetzner recommande de toujours passer `--remote-path=borg-1.4` pour éviter les incompatibilités de version. C'est donc un champ de configuration à exposer (ou à détecter).
- Authentification par clé SSH sans mot de passe. Attention : le format de clé publique attendu diffère selon le port (OpenSSH pour le port 23, RFC4716 pour le port 22) ; si vous voulez les deux, il faut les deux formats dans `authorized_keys`. **Chaque sous-compte a son propre `authorized_keys`** dans son répertoire.
- Pas de vrai shell sur la box : accès interactif limité sur le port 23, pas de pipes, pas de redirections, pas de scripts. Donc pas de `borg prune` déclenché côté serveur — c'est le client qui doit le faire.
- **Mode append-only** utilisable, mais à comprendre : un client restreint peut toujours *marquer* des archives comme supprimées ; la suppression réelle nécessite une opération depuis un client non restreint. C'est une protection anti-ransomware partielle, pas absolue.
- **Snapshots (ZFS) inclus** selon le plan, manuels ou planifiés. C'est un second filet de sécurité indépendant de Borg, très utile à mentionner dans la doc utilisateur.

### 2.3 Le problème Windows en détail

Quatre stratégies, à choisir consciemment :

| Stratégie | Comment | Avantages | Inconvénients |
|---|---|---|---|
| **A. WSL2** | L'app détecte/installe WSL2 + Ubuntu + borg, puis lance `wsl.exe -- borg ...` | Borg officiel, non modifié ; c'est la voie documentée par Vorta et WinBorg | Dépendance lourde (virtualisation, Hyper-V, droits admin) ; accès aux fichiers Windows via `/mnt/c` **lent** (9p) ; pas de VSS → les fichiers ouverts/verrouillés échouent ; métadonnées NTFS (ACL) perdues ; support « expérimental » |
| **B. Bundle Cygwin embarqué** (ex. `borg4win`) | ZIP autonome contenant borg + Python + OpenSSH + runtime Cygwin, embarqué dans votre binaire | Aucune dépendance à installer, portable, `borg` officiel non patché ; c'est la voie la plus simple pour « ça marche au premier lancement » | Pas de VSS ; pas d'ACL NTFS ; traduction de chemins `C:\x` → `/cygdrive/c/x` à gérer ; performances moyennes ; le projet de packaging a un très faible *bus factor* (à re-packager vous-même si besoin) |
| **C. Fork natif `borg-windows`** | Fork de Borg 1.4.4 avec support ACL NTFS (SDDL via API Win32), `borg.exe` autonome PyInstaller (~34 Mo) | Vraie intégration Windows (permissions sauvegardées/restaurées), pas de couche d'émulation, chemins natifs | **Fork non officiel, un seul mainteneur** ; pas de `borg mount` (pas de FUSE) ; pas de xattrs/ADS ; ajoute une clé `acl_windows` dans les items d'archive → **à tester impérativement** : une archive créée par le fork doit rester lisible par le Borg officiel sous Linux |
| **D. Changer de moteur sous Windows** (restic / kopia) | Un binaire Go natif, VSS supporté, backend SFTP compatible Storage Box | Vrai support Windows, mono-binaire par nature | Deux formats de dépôt à gérer, deux procédures de restauration, votre app n'est plus « une GUI Borg ». À n'envisager que si Windows devient la cible principale |

**Recommandation :** concevoir une interface interne `BorgRunner` (une seule fonction « exécute cette commande borg avec cet environnement, rends-moi le JSON ») avec deux implémentations, `NativeRunner` (Linux) et `WindowsRunner` (B ou C). Vous pouvez alors démarrer sur **B** (le plus rapide à faire marcher) et basculer sur **C** sans toucher à l'UI.

**Point critique à ne pas oublier : pas de VSS = fichiers verrouillés non sauvegardés.** Outlook (.pst/.ost), bases SQLite ouvertes, machines virtuelles en cours d'exécution échoueront. Mitigation possible en v1.1 : créer un cliché VSS via `diskshadow`/`vshadow`, le monter, sauvegarder depuis le point de montage, le démonter. C'est du travail non trivial mais c'est ce qui sépare un jouet d'un outil de sauvegarde poste de travail.

---

## 3. Choix du langage et de la bibliothèque UI

### 3.1 Ce dont le programme a réellement besoin

C'est un cahier des charges modeste, et ça pèse sur le choix :

1. Lancer un processus externe, lire stdout/stderr en flux, parser du JSON ligne par ligne (`--log-json`)
2. Afficher 4 écrans de formulaires + une liste + une barre de progression
3. Stocker une configuration et un historique local
4. Stocker un secret (passphrase du dépôt) dans le trousseau de l'OS
5. Créer une tâche planifiée (Task Scheduler / systemd user timer)
6. Optionnellement : tester une connexion SSH et déposer une clé publique
7. Embarquer et extraire un runtime Borg

Aucun besoin de rendu graphique complexe, d'animations, ni de web. **La difficulté est dans l'orchestration, pas dans l'UI.**

### 3.2 Comparatif

| Pile | Binaire unique | Deps runtime Linux | Taille | Effort dev | Qualité UI | Verdict |
|---|---|---|---|---|---|---|
| **Go + Fyne** (BSD-3) | Oui (1 fichier) | libc, X11/Wayland, OpenGL — présents partout | ~20–40 Mo | Faible | Correcte, Material-ish, un peu générique | **Recommandé** |
| Go + Wails (MIT) | Non vraiment | **WebKitGTK** requis | ~15 Mo | Moyen | Excellente (HTML/CSS) | Packaging Linux pénible |
| Rust + egui/eframe (MIT/Apache) | Oui | idem Fyne | ~10 Mo | Moyen (Rust) | Fonctionnelle mais look « immediate mode » | Bon 2ᵉ choix si vous aimez Rust |
| Rust + Tauri (MIT/Apache) | Non | **WebKitGTK** | ~10 Mo | Élevé | Excellente | Surdimensionné ici |
| **C# .NET 9 + Avalonia** (MIT) | Oui (`PublishSingleFile` + `--self-contained`) | ICU/openssl selon config | ~40–70 Mo | Faible | Très bonne (XAML, MVVM) | **Meilleure alternative** |
| Python + PySide6 (LGPL) | Via PyInstaller, ~80–150 Mo, démarrage lent | Nombreuses | Gros | Très faible | Très bonne (Qt) | Prototypage rapide ; c'est le choix de Vorta ; attention LGPL si distribution fermée |
| C++ / Qt Widgets | Possible mais laborieux | Qt | Gros | Élevé | Très bonne | Pas justifié |
| Electron | Non | — | 150 Mo+ | Faible | Excellente | Non |

### 3.3 Recommandation : Go + Fyne

**Pourquoi Go :**
- `os/exec` + `encoding/json` + `bufio.Scanner` : le cœur du programme (piloter borg et lire sa sortie JSON) fait quelques centaines de lignes, sans dépendance.
- `golang.org/x/crypto/ssh` : tester la connexion à la Storage Box, vérifier l'empreinte du serveur, déposer la clé publique — sans dépendre d'un binaire `ssh` externe pour la partie « diagnostic ».
- `go:embed` : embarquer le runtime Borg Windows dans l'exécutable, l'extraire au premier lancement (avec vérification SHA-256). C'est ce qui rend l'objectif « un seul fichier » réel.
- Compilation croisée triviale pour la partie non-GUI, un seul artefact par plateforme, pas de runtime à installer.

**Pourquoi Fyne :**
- Série 2.7 active (2.7.4 en mai 2026), Wayland supporté par défaut, Go 1.22+ minimum.
- Widgets suffisants et bien nommés pour ce projet : `widget.Form`, `widget.List`, `widget.ProgressBar`, `dialog.ShowFolderOpen`, `widget.AppTabs`, icône de barre système.
- Un seul binaire, pas de webview, pas de runtime.

**Les deux frictions à connaître :**
1. **Fyne utilise CGO** → la compilation croisée nécessite `fyne-cross` (Docker) ou un runner CI par OS. Prévoyez un GitHub Actions avec `windows-latest` + `ubuntu-latest`, c'est plus simple que le cross-compile.
2. L'esthétique de Fyne est reconnaissable et peu personnalisable en profondeur. Pour une UI « très simple », c'est un non-problème ; si le look est un critère fort, prenez Avalonia.

**Si votre équipe est .NET** : Avalonia est objectivement le meilleur compromis productivité/qualité d'UI, avec `dotnet publish -r win-x64 / linux-x64 -p:PublishSingleFile=true --self-contained` qui produit un fichier unique **sans Docker**, y compris en cross-publish depuis une seule machine. Le prix est la taille (~40–70 Mo) et un temps de démarrage un peu supérieur.

---

## 4. Architecture proposée

```
┌──────────────────────────────────────────────┐
│  GUI (Fyne)  — 4 écrans, aucun appel direct  │
│              à borg                          │
└───────────────┬──────────────────────────────┘
                │  (channels / events)
┌───────────────▼──────────────────────────────┐
│  Core                                        │
│  ├─ ConfigStore    (TOML)                    │
│  ├─ SecretStore    (trousseau OS)            │
│  ├─ HistoryStore   (SQLite ou JSONL)         │
│  ├─ Scheduler      (schtasks / systemd)      │
│  └─ BorgRunner     ← interface               │
│       ├─ NativeRunner   (Linux)              │
│       └─ WindowsRunner  (Cygwin ou fork)     │
└───────────────┬──────────────────────────────┘
                │  exec + parse --log-json
┌───────────────▼──────────────────────────────┐
│  borg 1.4  ──ssh:23──►  Hetzner Storage Box  │
└──────────────────────────────────────────────┘
```

### 4.1 Le binaire est aussi son propre agent

Un seul exécutable, deux modes :

- `borgui` → lance la GUI
- `borgui --run <profil>` → exécute la sauvegarde sans UI, écrit le résultat dans l'historique, notifie
- `borgui --check <profil>`, `borgui --install-schedule <profil>`

La tâche planifiée appelle le binaire en mode `--run`. Conséquence : pas de démon séparé, pas de second exécutable, et la sauvegarde continue même si la GUI n'est jamais ouverte. C'est le point d'architecture le plus important pour tenir l'objectif « un binaire ».

### 4.2 Configuration

Fichier unique, lisible et éditable à la main (c'est un avantage, pas un défaut, pour du logiciel de sauvegarde) :

- Linux : `~/.config/borgui/config.toml`
- Windows : `%APPDATA%\borgui\config.toml`

```toml
[[profile]]
name          = "Poste de travail"
repo          = "ssh://u123456@u123456.your-storagebox.de:23/./bureau"
remote_path   = "borg-1.4"          # Hetzner : 1.2 ou 1.4
ssh_key       = "~/.config/borgui/id_ed25519"
compression   = "zstd,3"
sources       = ["/home/marc/Documents", "/home/marc/Projets"]
excludes      = ["**/node_modules", "**/.cache", "**/*.iso", "**/Trash"]
exclude_caches = true               # respecte CACHEDIR.TAG
one_file_system = true

[profile.retention]
daily = 7
weekly = 4
monthly = 6

[profile.schedule]
kind = "daily"
at   = "12:30"
catch_up_if_missed = true           # essentiel pour les portables
```

### 4.3 Secrets

- Passphrase du dépôt → **trousseau de l'OS** (Credential Manager sous Windows, Secret Service/libsecret sous Linux ; bibliothèque `zalando/go-keyring`). Repli : fichier en 0600 avec avertissement clair dans l'UI.
- Transmission à Borg : **`BORG_PASSCOMMAND`** pointant sur `borgui --print-passphrase <profil>` plutôt que `BORG_PASSPHRASE` dans l'environnement.
- Clé SSH dédiée à l'application, `ed25519`, sans passphrase (sinon la sauvegarde planifiée demandera une interaction), générée par l'app.
- `known_hosts` dédié à l'application, avec épinglage de l'empreinte au moment de l'appariement, et refus silencieux ensuite (`StrictHostKeyChecking=yes`).

### 4.4 Séquence de commandes Borg

Toutes les commandes reçoivent `--remote-path=borg-1.4` et un environnement contenant `BORG_REPO`, `BORG_PASSCOMMAND`, `BORG_RSH="ssh -i <clé> -p 23 -o UserKnownHostsFile=<...>"`.

Initialisation (une seule fois, par l'assistant) :
```
borg init --encryption=repokey-blake2
```
→ **Puis immédiatement `borg key export --paper` et forcer l'utilisateur à sauvegarder/imprimer la clé.** Sans passphrase *et* sans clé, les données sont définitivement perdues ; l'UI doit rendre cette étape impossible à sauter.

Sauvegarde :
```
borg create --stats --json --log-json --progress \
  --compression zstd,3 --one-file-system --exclude-caches \
  --exclude-from <fichier généré> \
  ::'{hostname}-{now:%Y-%m-%dT%H:%M:%S}' <sources...>
```

Rotation, puis récupération d'espace :
```
borg prune --list --json --keep-daily 7 --keep-weekly 4 --keep-monthly 6
borg compact
```

Lecture d'état (pour l'écran « État », sans rien modifier) :
```
borg list --json
borg info --json
borg check --repository-only        # mensuel, en tâche de fond
```

Points d'implémentation :
- Codes de retour Borg : `0` = succès, `1` = **avertissement** (typiquement des fichiers illisibles — la sauvegarde existe quand même), `2` = erreur. Traiter `1` comme « Terminé avec des avertissements » en orange, pas comme un échec rouge.
- `--log-json` donne des événements `progress_percent`, `archive_progress`, `log_message` : c'est ce qui alimente la barre de progression.
- Verrous : après un crash, le dépôt reste verrouillé. Détecter l'erreur `LockTimeout` et proposer un bouton unique « Débloquer le dépôt » (`borg break-lock`) avec une explication en français.
- Un seul processus Borg à la fois par dépôt (verrou local en plus du verrou distant).

### 4.5 Planification

| OS | Mécanisme | Commande |
|---|---|---|
| Windows | Task Scheduler | `schtasks /Create /TN "BorgUI - <profil>" /TR "\"<exe>\" --run <profil>" /SC DAILY /ST 12:30` (+ `/RL HIGHEST` si sauvegarde de fichiers système) |
| Linux | systemd **user** timer (`~/.config/systemd/user/`) + `Persistent=true` | `systemctl --user enable --now borgui@<profil>.timer` |

`Persistent=true` (systemd) et l'option « rattraper si manqué » (Windows) sont indispensables pour un poste éteint la nuit.

---

## 5. L'UI : maximum 4 écrans

Le but déclaré étant la simplicité, la règle est : **le vocabulaire de Borg ne doit jamais apparaître à l'écran.** Pas de « repository », « archive », « prune », « chunker ». On dit « destination », « sauvegarde », « conservation ».

### Écran 1 — État (écran d'accueil par défaut)

```
┌────────────────────────────────────────────────────────┐
│  ✅  Dernière sauvegarde réussie                       │
│      hier à 12h31 · 1 240 fichiers · 340 Mo envoyés    │
│      Prochaine sauvegarde : aujourd'hui à 12h30        │
│                                                        │
│      [ Sauvegarder maintenant ]   [ Restaurer… ]       │
│                                                        │
│  Historique                                            │
│   ✅ 02/09 12:31   3 min 12 s    340 Mo                │
│   ⚠️ 01/09 12:30   4 min 02 s    512 Mo   3 fichiers…  │
│   ✅ 31/08 12:30   2 min 55 s    120 Mo                │
│   ❌ 30/08 12:30   —             Serveur injoignable   │
│                                                        │
│  Espace occupé sur la destination : 42,1 Go            │
└────────────────────────────────────────────────────────┘
```

Un clic sur une ligne en échec ouvre l'explication et le journal complet. Un seul indicateur dominant en haut : **vert / orange / rouge**, avec une phrase en français, jamais un code d'erreur nu.

### Écran 2 — Ce que je sauvegarde

Deux listes, deux boutons `+` / `−`. Rien d'autre.
- Dossiers : sélecteur de dossier natif ; afficher la taille estimée à côté de chaque entrée (calcul en tâche de fond).
- Exclusions : champ texte libre + **des cases à cocher de préréglages** (« Fichiers temporaires », « Caches de navigateurs », « node_modules », « Images disque ISO/VMDK », « Corbeille »). C'est là que 95 % des utilisateurs s'arrêteront, et c'est très bien.

### Écran 3 — Destination

Un formulaire avec un choix en haut : `Hetzner Storage Box` / `Autre serveur SSH`. En mode Hetzner, on ne demande que : **nom d'utilisateur** (`u123456`), **nom du dépôt** (« bureau »), **version de Borg** (liste : 1.4 par défaut, 1.2), et la **passphrase**. Le reste (hôte `u123456.your-storagebox.de`, port 23, chemin `/./`) est déduit automatiquement.

Un bouton **« Tester la connexion »** qui vérifie, dans l'ordre, et affiche le premier point qui casse : résolution DNS → port 23 ouvert → clé SSH acceptée → `borg` présent à la version demandée → dépôt lisible avec cette passphrase. C'est le bouton le plus important de toute l'application.

Un bouton **« Copier ma clé publique »** + un rappel de l'action à faire dans la console Hetzner (activer SSH support et External reachability, coller la clé).

### Écran 4 — Réglages

Fréquence (quotidienne / hebdomadaire / manuelle + heure), conservation (3 champs : jours / semaines / mois, avec une phrase explicative générée : « vous pourrez revenir en arrière jusqu'à 6 mois »), compression (3 choix nommés : Rapide / Équilibré / Maximum), limite de débit montant, notifications.

### Assistant de premier lancement

5 étapes, une question par écran : *Bienvenue → Où sauvegarder ? (+ test) → Quoi sauvegarder ? → Quand ? → Votre clé de secours (obligatoire) → Première sauvegarde*.

### Règles UX transversales

- Ne jamais bloquer l'UI : tout appel à Borg est asynchrone, annulable, avec progression.
- Chaque erreur Borg est traduite : un dictionnaire d'erreurs connues → message français + action proposée, avec le journal brut disponible en dépliant « Détails ».
- L'application doit fonctionner correctement si l'utilisateur n'ouvre jamais l'écran 4.

---

## 6. Risques et pièges

| Risque | Gravité | Mitigation |
|---|---|---|
| Perte de la passphrase / clé du dépôt | **Critique — données irrécupérables** | Export de la clé (`borg key export --paper`) obligatoire dans l'assistant, avec confirmation ; rappel périodique |
| Restauration jamais testée | **Critique** | Bouton « Vérifier une restauration » qui extrait un fichier au hasard dans un dossier temporaire et compare ; relance mensuelle automatique |
| Fichiers verrouillés non sauvegardés (Windows sans VSS) | Élevé | Documenter clairement ; afficher la liste des fichiers ignorés après chaque exécution ; VSS en v1.1 |
| Dépendance à un fork ou à un packaging Cygwin peu maintenu | Élevé | Vérifier en CI qu'une archive créée sous Windows est lisible par le Borg officiel sous Linux ; documenter la procédure de restauration de secours depuis un live-USB Linux |
| `prune` mal configuré = perte d'historique | Élevé | Toujours `--list --dry-run` affiché avant confirmation lors d'un changement de rétention |
| Dépôt verrouillé après un crash | Moyen | Détection + bouton « Débloquer » |
| Portable éteint à l'heure planifiée | Moyen | `Persistent=true` / rattrapage, et déclenchement au retour du réseau |
| Corruption d'un chunk dédupliqué affectant toutes les archives | Moyen | `borg check` mensuel ; snapshots Hetzner activés ; recommander un second dépôt pour les données vitales |
| Chemins Windows > 260 caractères, caractères invalides, ADS | Moyen | Tester tôt, journaliser les échecs par fichier |

---

## 7. Lotissement suggéré

**v0.1 — Preuve de faisabilité (le plus incertain d'abord)**
Aucune UI. Un binaire CLI en Go qui, sur Windows, extrait le runtime Borg embarqué et réussit un `borg init` + `borg create` + `borg list` vers une vraie Storage Box. Mesurer les performances sur 20 Go. **Si cette étape échoue, tout le reste est inutile** — d'où l'ordre.

**v0.2 — MVP GUI**
Écrans 1, 2, 3 + assistant + bouton « Sauvegarder maintenant » + historique local. Un seul profil.

**v0.3 — Automatisation**
Planification (schtasks / systemd user), notifications, icône de barre système, mode `--run`.

**v1.0 — Confiance**
Restauration guidée (parcourir une archive, extraire une sélection), `borg check` planifié, test de restauration automatique, export de la clé, traduction des erreurs.

**v1.1+ — Robustesse Windows**
VSS, ACL NTFS, profils multiples, plusieurs destinations, hooks pré/post (dump de base de données).

Ordre de grandeur pour un développeur seul déjà à l'aise en Go : v0.1 en quelques jours, v0.2 en 2–3 semaines, v1.0 autour de 2 mois. La partie GUI n'est pas le poste principal ; la gestion des cas d'erreur et le packaging Windows le sont.

---

## 8. À évaluer avant d'écrire du code

| Projet | Pile | Plateformes | Intérêt pour vous |
|---|---|---|---|
| **Vorta** | Python + PyQt | Linux, macOS | La GUI Borg de référence. Windows uniquement via WSL. À regarder pour le modèle de données et l'UX. Un fork/contribution est peut-être moins coûteux qu'un projet neuf. |
| **Pika Backup** | GTK4/Rust | Linux | Le meilleur exemple de « UI très simple » pour Borg. À copier sur le plan UX. |
| **WinBorg** | Windows | Windows | GUI Windows pilotant Borg dans WSL2, avec onboarding guidé. Projet récent : à tester pour voir si ça suffit déjà à votre besoin. |
| **borgmatic** | Python (CLI) | Unix | Enveloppe déclarative (YAML) qui enchaîne create/prune/compact/check. Envisageable comme couche moteur, mais ajoute une dépendance Python. |

Si l'un de ces outils couvre votre besoin sous Linux, la question se réduit à : « ai-je vraiment besoin de Windows, et si oui, une GUI Windows dédiée ne serait-elle pas plus simple qu'une application unifiée ? »

---

## 9. Questions ouvertes qui changent l'architecture

1. **Windows : quelle part réelle du besoin ?** Si c'est marginal, stratégie B (Cygwin embarqué) et on avance. Si c'est la cible principale, il faut sérieusement peser la stratégie D (restic/kopia).
2. **Un seul poste par utilisateur, ou une flotte à administrer ?** Une flotte impose une configuration centralisée et de la supervision — architecture très différente.
3. **Sauvegarde des données utilisateur, ou du système entier ?** Le système entier implique des droits admin/root, VSS, et une planification en tant que SYSTEM.
4. **Distribution ouverte ou fermée ?** Impacte les licences (Qt/PySide6 en LGPL demande des précautions ; Fyne en BSD-3 et Avalonia en MIT, non).
5. **Faut-il la restauration dans la v1 ?** Une app de sauvegarde sans restauration intégrée est difficilement défendable, mais c'est ~30 % du travail d'UI.
6. **Un dépôt par machine, ou un sous-compte Hetzner par machine ?** Les sous-comptes isolent mieux (chaque sous-compte a son `authorized_keys` et son répertoire) et permettent l'append-only par machine.

---

## 10. Décision, en une phrase

**Go + Fyne, ciblant Borg 1.4, avec un runtime Borg embarqué dans le binaire pour Windows (Cygwin d'abord, fork natif ensuite si les tests le valident), une interface `BorgRunner` pour isoler cette incertitude, le même binaire servant d'agent planifié, et une UI de 4 écrans où le mot « repository » n'apparaît jamais.**

Et avant tout : **la v0.1 doit être un test de bout en bout sur une vraie Storage Box, sans UI**, parce que c'est là que se trouve le risque du projet.
