# Cahier des charges

**Projet :** application desktop de configuration et de supervision de sauvegardes BorgBackup
**Nom de travail :** BorgUI
**Version du document :** 1.0 — 3 septembre 2026
**Statut :** à valider

---

## 1. Contexte et objectif

### 1.1 Besoin

Sauvegarder les données utilisateur de quelques postes de travail Windows et Linux vers une Storage Box Hetzner, en utilisant BorgBackup comme moteur, sans exiger de l'utilisateur qu'il manipule une ligne de commande ni qu'il comprenne le vocabulaire de Borg.

### 1.2 Objectif du logiciel

Fournir une interface graphique **délibérément minimale** permettant de :

1. configurer la destination des sauvegardes (une Storage Box Hetzner, ou un serveur SSH quelconque) ;
2. désigner les dossiers à sauvegarder et les exclusions ;
3. consulter l'état des dernières sauvegardes de l'ensemble des postes ;
4. restaurer des fichiers.

### 1.3 Critère de réussite principal

Un utilisateur non technique doit pouvoir installer l'application et obtenir une première sauvegarde fonctionnelle et planifiée **en moins de dix minutes, sans documentation**.

### 1.4 Principe directeur

L'application est une **couche de configuration et de supervision au-dessus de BorgBackup**, pas une réimplémentation. Les dépôts produits sont des dépôts Borg standard, lisibles par n'importe quelle installation de Borg 1.4. En cas d'abandon du projet, aucune donnée n'est captive.

---

## 2. Périmètre

### 2.1 Dans le périmètre

- Postes de travail Windows 10/11 (x86-64) et Linux de bureau (x86-64).
- Données utilisateur : documents, projets, photos, configurations personnelles.
- Un profil de sauvegarde par poste, une destination par profil.
- Quelques postes (ordre de grandeur : 2 à 20), configurés individuellement.
- Chiffrement des sauvegardes **facultatif**.

### 2.2 Hors périmètre (explicitement exclu de la v1)

| Exclusion | Motif |
|---|---|
| Sauvegarde du système d'exploitation, image disque, bare-metal restore | Périmètre « données utilisateur » |
| VSS / clichés instantanés Windows | Non nécessaire hors fichiers système ; réserve documentée sur Outlook |
| Préservation des ACL NTFS | Restauration dans le profil du même utilisateur : héritage suffisant |
| Droits administrateur, service Windows, tâche SYSTEM | L'application tourne en utilisateur courant |
| Administration centralisée, serveur de gestion, inventaire | Périmètre « configuration manuelle » |
| Plusieurs profils ou plusieurs destinations par poste | Complexité disproportionnée pour le besoin |
| Hooks pré/post (dump de base de données, arrêt de service) | Cas serveur, hors périmètre |
| macOS | Non demandé |
| Montage d'archive (`borg mount`) | Impossible sous Windows, non retenu pour garantir la parité |
| Borg 2.x | Encore en beta, non supporté par Hetzner |

---

## 3. Décisions techniques imposées

Ces choix sont arrêtés et ne font pas partie des arbitrages du prestataire ou du développeur.

| Réf. | Décision |
|---|---|
| DT-01 | Langage : **Go** (1.22 minimum) |
| DT-02 | Bibliothèque UI : **Fyne 2.7.x** |
| DT-03 | Moteur de sauvegarde : **BorgBackup 1.4.x**, non modifié |
| DT-04 | Sous Linux : Borg fourni par la distribution, ou binaire officiel |
| DT-05 | Sous Windows : runtime Borg basé sur **Cygwin** (code Borg officiel), **téléchargé au premier lancement**, non embarqué dans l'exécutable |
| DT-06 | Le chiffrement des dépôts est **facultatif** : modes `repokey-blake2` ou `none` |
| DT-07 | **Un seul exécutable** par plateforme, servant à la fois d'interface et d'agent de sauvegarde planifiée |
| DT-08 | Un dépôt Borg et un sous-compte Hetzner **par poste** ; pas de dépôt mutualisé |
| DT-09 | Configuration en **TOML**, lisible et modifiable à la main |
| DT-10 | Historique et cache en **SQLite** (`modernc.org/sqlite`, sans CGO additionnel) |
| DT-11 | Restauration fondée sur `borg list` + `borg extract`, **jamais** sur `borg mount` |
| DT-12 | Aucun droit administrateur requis à l'installation comme à l'exécution |

---

## 4. Exigences fonctionnelles

Priorités : **O** = obligatoire v1 · **I** = important · **S** = souhaité (peut glisser)

### 4.1 Installation et premier lancement

| Réf. | Exigence | Prio |
|---|---|---|
| EF-01 | L'application s'installe et s'exécute sans droits administrateur. | O |
| EF-02 | Au premier lancement sous Windows, l'application télécharge le runtime Borg depuis une URL versionnée, en HTTPS, avec barre de progression et possibilité de reprise après échec. | O |
| EF-03 | L'empreinte SHA-256 du runtime est compilée dans l'exécutable et vérifiée avant toute extraction. Un écart interrompt l'installation avec un message explicite. | O |
| EF-04 | L'installation du runtime est atomique : extraction en répertoire temporaire, puis renommage vers `%LOCALAPPDATA%\borgui\runtime\<version>\`. | O |
| EF-05 | L'installation est validée fonctionnellement (`borg --version` retourne la version attendue) avant d'être déclarée réussie. | O |
| EF-06 | En cas d'impossibilité de télécharger (absence de réseau, proxy, filtrage), l'application propose une **voie hors ligne** : téléchargement manuel d'une archive à déposer dans un dossier indiqué, vérifiée par la même empreinte. | O |
| EF-07 | Les variables de proxy système sont respectées. | I |
| EF-08 | Sous Linux, l'application détecte la présence de `borg` et sa version. Si absent ou incompatible, elle affiche la commande d'installation propre à la distribution détectée. | O |
| EF-09 | La version de Borg est épinglée par version de l'application. Aucune mise à jour automatique du moteur. Un bouton explicite « Mettre à jour le moteur » est proposé, avec la version cible affichée. | O |

### 4.2 Assistant de première configuration

| Réf. | Exigence | Prio |
|---|---|---|
| EF-10 | Un assistant guide le premier lancement en une question par écran : bienvenue → moteur → destination → chiffrement → dossiers → planification → clé de secours (si applicable) → première sauvegarde. | O |
| EF-11 | L'assistant est interruptible et reprenable : l'état est persisté à chaque étape. | I |
| EF-12 | Aucune étape ne demande de connaissance de Borg, de SSH ou de chiffrement pour être franchie. | O |
| EF-13 | Une configuration exportée depuis un autre poste peut être importée en début d'assistant ; seuls le sous-compte et, le cas échéant, la passphrase sont alors demandés. | I |

### 4.3 Destination

| Réf. | Exigence | Prio |
|---|---|---|
| EF-20 | L'utilisateur choisit entre « Hetzner Storage Box » et « Autre serveur SSH ». | O |
| EF-21 | En mode Hetzner, seuls sont demandés : nom d'utilisateur (`uXXXXXX`), nom du dépôt, version de Borg côté serveur (1.4 par défaut, 1.2 proposé). L'hôte, le port 23 et le chemin relatif `/./` sont déduits automatiquement. | O |
| EF-22 | Le paramètre `--remote-path` est transmis à toutes les commandes Borg, conformément à la recommandation de Hetzner. | O |
| EF-23 | L'application génère une clé SSH `ed25519` dédiée, sans passphrase, et propose de copier la clé publique dans le presse-papiers. | O |
| EF-24 | L'application rappelle les actions à effectuer dans la console Hetzner : activer « SSH support » et « External reachability », déposer la clé publique. | O |
| EF-25 | Un bouton **« Tester la connexion »** vérifie séquentiellement : résolution DNS, accessibilité du port 23, acceptation de la clé SSH, présence de Borg à la version demandée, lisibilité du dépôt. Le premier point en échec est affiché avec l'action corrective correspondante. | O |
| EF-26 | L'empreinte du serveur est épinglée lors de la première connexion dans un fichier `known_hosts` propre à l'application. Toute divergence ultérieure fait échouer la connexion avec un avertissement de sécurité explicite. | O |
| EF-27 | Une limite de débit montant est configurable (`--upload-ratelimit`). | S |

### 4.4 Chiffrement

| Réf. | Exigence | Prio |
|---|---|---|
| EF-30 | Deux modes seulement sont exposés : « Chiffré » (`repokey-blake2`, proposé par défaut) et « Non chiffré » (`none`). Les modes intermédiaires de Borg ne sont pas exposés. | O |
| EF-31 | Le choix est présenté **une seule fois**, sur un écran dédié de l'assistant, avec l'énoncé des deux conséquences : confidentialité vis-à-vis de l'hébergeur d'une part, risque de perte définitive en cas de passphrase égarée d'autre part. | O |
| EF-32 | Le choix est présenté comme **irréversible** : un changement de mode impose la création d'un nouveau dépôt et la perte de l'historique. | O |
| EF-33 | Le mode est affiché mais **non modifiable** ailleurs dans l'application. | O |
| EF-34 | Pour un dépôt existant, le mode est **lu** via `borg info --json` et jamais redemandé à l'utilisateur. | O |
| EF-35 | En mode chiffré, l'export de la clé de secours (`borg key export --paper`) est **obligatoire et non contournable** avant la première sauvegarde, avec confirmation explicite de l'utilisateur. Un rappel est affiché à chaque lancement tant que la confirmation n'est pas acquise. | O |
| EF-36 | En mode chiffré, la passphrase est stockée dans le trousseau du système (Credential Manager, Secret Service). En cas d'indisponibilité du trousseau, un repli sur fichier en permissions restreintes est possible, avec avertissement visible dans l'interface. | O |
| EF-37 | La passphrase est transmise à Borg par `BORG_PASSCOMMAND` invoquant l'application elle-même, jamais par `BORG_PASSPHRASE` dans l'environnement. | O |
| EF-38 | En mode non chiffré, `BORG_UNKNOWN_UNENCRYPTED_REPO_ACCESS_IS_OK=yes` est systématiquement positionné, afin qu'aucune exécution planifiée ne puisse se bloquer sur une question interactive. | O |
| EF-39 | En mode non chiffré, aucune entrée n'est créée dans le trousseau et l'étape de clé de secours est omise. | O |

### 4.5 Sélection des données

| Réf. | Exigence | Prio |
|---|---|---|
| EF-40 | L'utilisateur ajoute et retire des dossiers via un sélecteur natif du système. | O |
| EF-41 | La taille de chaque dossier sélectionné est estimée et affichée, en tâche de fond, sans bloquer l'interface. | I |
| EF-42 | Les exclusions sont saisissables comme motifs libres et proposées sous forme de **préréglages cochables** : fichiers temporaires, caches de navigateurs, `node_modules`, images disque (ISO, VMDK, VDI), corbeille, `AppData\Roaming` hors caches connus, fichiers `.ost` d'Outlook. | O |
| EF-43 | Les exclusions sont stockées comme motifs portables (`**/node_modules`) et jamais comme chemins absolus, afin de rester valides sur les deux plateformes. | O |
| EF-44 | `--exclude-caches` (respect de `CACHEDIR.TAG`) est actif par défaut. | I |
| EF-45 | Sous Windows, les fichiers en espace réservé cloud (OneDrive, Dropbox « fichiers à la demande », attribut `FILE_ATTRIBUTE_RECALL_ON_DATA_ACCESS`) sont **exclus par défaut**, afin de ne pas déclencher leur téléchargement intégral. Le comportement est signalé dans l'interface et modifiable. | O |
| EF-46 | Un avertissement explicite est affiché si un fichier `.pst` Outlook est inclus dans le périmètre. | I |
| EF-47 | `--one-file-system` est configurable, actif par défaut sous Linux. | S |

### 4.6 Exécution des sauvegardes

| Réf. | Exigence | Prio |
|---|---|---|
| EF-50 | Un bouton « Sauvegarder maintenant » déclenche une sauvegarde immédiate. | O |
| EF-51 | La progression est affichée en temps réel (pourcentage, fichier en cours, volume transféré), à partir des événements `--log-json`. | O |
| EF-52 | Une sauvegarde en cours est annulable proprement. | I |
| EF-53 | Le nom d'archive suit le motif `{hostname}-{now:%Y-%m-%dT%H:%M:%S}`. | O |
| EF-54 | Sous Windows, `borg create` est exécuté avec le répertoire courant positionné sur `/cygdrive`, en passant des chemins relatifs préfixés de la lettre de lecteur (`c/Users/marc/Documents`), afin que l'archive contienne la lettre de lecteur sans préfixe parasite. | O |
| EF-55 | Après chaque sauvegarde, la rotation est appliquée (`borg prune`) puis l'espace récupéré (`borg compact`). | O |
| EF-56 | Le code de retour 1 de Borg est traité comme « terminé avec avertissements » (état orange) et non comme un échec. La liste des fichiers ignorés est consultable. | O |
| EF-57 | Un verrou local empêche deux exécutions simultanées sur le même dépôt. | O |
| EF-58 | En cas de dépôt verrouillé après un arrêt brutal, l'application détecte la situation et propose un bouton unique « Débloquer le dépôt » (`borg break-lock`) accompagné d'une explication. | O |
| EF-59 | La compression est configurable via trois libellés : Rapide (`lz4`), Équilibré (`zstd,3`, par défaut), Maximum (`zstd,9`). | I |

### 4.7 Planification

| Réf. | Exigence | Prio |
|---|---|---|
| EF-60 | L'application installe et désinstalle la tâche planifiée : Planificateur de tâches sous Windows (`schtasks`), timer systemd utilisateur sous Linux. | O |
| EF-61 | Fréquences proposées : quotidienne, hebdomadaire, manuelle, avec choix de l'heure. | O |
| EF-62 | Le rattrapage des exécutions manquées est actif (`Persistent=true` sous systemd, option équivalente sous Windows) : un poste éteint à l'heure prévue sauvegarde à son démarrage suivant. | O |
| EF-63 | La tâche planifiée invoque le même exécutable en mode `--run <profil>`, sans interface. | O |
| EF-64 | Une notification système signale les échecs. Les succès sont silencieux par défaut. | I |
| EF-65 | Une icône de barre système donne accès à l'état et au déclenchement manuel. | S |

### 4.8 Rétention

| Réf. | Exigence | Prio |
|---|---|---|
| EF-70 | Trois champs de rétention : nombre de jours, de semaines, de mois à conserver (défaut 7 / 4 / 6). | O |
| EF-71 | Une phrase générée traduit le réglage en langage courant : « vous pourrez revenir en arrière jusqu'à 6 mois ». | I |
| EF-72 | Toute modification de la rétention affiche le résultat d'un `borg prune --list --dry-run` — c'est-à-dire la liste des archives qui seraient supprimées — avant confirmation. | O |

### 4.9 État et supervision

| Réf. | Exigence | Prio |
|---|---|---|
| EF-80 | L'écran d'accueil affiche un indicateur dominant unique (vert / orange / rouge) accompagné d'une phrase en français, jamais d'un code d'erreur brut. | O |
| EF-81 | Sont affichés : date et résultat de la dernière sauvegarde, durée, nombre de fichiers, volume transféré, date de la prochaine exécution, espace occupé sur la destination. | O |
| EF-82 | Un historique des exécutions est conservé localement et consultable ; chaque ligne en échec ouvre l'explication et le journal complet. | O |
| EF-83 | À la fin de chaque exécution, un fichier d'état `status/<hostname>.json` est déposé sur la destination par SFTP, contenant : nom du poste, date, résultat, durée, volume, taille du dépôt, mode de chiffrement, prochaine exécution. | I |
| EF-84 | ~~L'écran d'accueil agrège les fichiers d'état disponibles et affiche l'état de tous les postes du parc.~~ **Retirée** le 24 septembre 2026 : l'état d'un poste n'est pas visible des autres (addendum §8). | — |
| EF-85 | Le poste courant, si son fichier d'état n'a pas été mis à jour depuis plus de 48 heures, est signalé en orange, avec la mention « aucune sauvegarde depuis N jours ». | I |
| EF-86 | Les fichiers d'état sont traités comme des données non fiables au parsing. | O |
| EF-87 | Un `borg check --repository-only` est exécuté mensuellement en tâche de fond ; son résultat est affiché. | I |

### 4.10 Restauration

| Réf. | Exigence | Prio |
|---|---|---|
| EF-90 | L'utilisateur sélectionne une archive dans la liste des sauvegardes disponibles (`borg list --json`). | O |
| EF-91 | Le contenu de l'archive est navigable sous forme d'arborescence. | O |
| EF-92 | Une recherche par nom de fichier est disponible dans l'archive sélectionnée. | O |
| EF-93 | La liste des fichiers d'une archive est mise en cache localement (SQLite) au premier accès. Les archives étant immuables, ce cache n'est jamais invalidé. | O |
| EF-94 | La restauration partielle (un fichier, un dossier) est aussi accessible que la restauration complète. | O |
| EF-95 | La destination de restauration par défaut est un **dossier neuf** (`Restauration <date>`), jamais l'emplacement d'origine. | O |
| EF-96 | La restauration à l'emplacement d'origine est possible, derrière une confirmation explicite énonçant ce qui sera écrasé. | O |
| EF-97 | Sous Windows, l'extraction est réalisée depuis la racine du lecteur avec `--strip-components 1`, conformément à la convention d'écriture des archives. | O |
| EF-98 | Une option « tout récupérer dans un seul fichier » utilise `borg export-tar`. | S |
| EF-99 | Une fois par mois, l'application extrait automatiquement un fichier tiré au hasard dans un répertoire temporaire, compare son contenu à l'original lorsque celui-ci existe encore, et affiche le résultat sur l'écran d'accueil (« Restauration vérifiée le … »). | O |

### 4.11 Portabilité de la configuration

| Réf. | Exigence | Prio |
|---|---|---|
| EF-100 | « Exporter la configuration » produit un fichier TOML **sans aucun secret**. | I |
| EF-101 | « Importer une configuration » préremplit tous les champs et ne demande que le sous-compte et, le cas échéant, la passphrase. | I |

---

## 5. Exigences d'interface

| Réf. | Exigence | Prio |
|---|---|---|
EI-01 | L'application comporte **au maximum quatre écrans** : État, Sauvegarde (dossiers et exclusions), Destination, Réglages. La restauration est une fenêtre dédiée. | O |
| EI-02 | Le vocabulaire de Borg n'apparaît **jamais** dans l'interface : ni « repository », ni « archive », ni « prune », ni « chunk ». On dit destination, sauvegarde, conservation. | O |
| EI-03 | Aucune opération ne bloque l'interface. Tout appel à Borg est asynchrone, avec progression et annulation lorsque c'est possible. | O |
| EI-04 | Chaque erreur Borg connue est traduite en un message français accompagné d'une action proposée. Le journal brut reste accessible sous un dépliant « Détails ». | O |
| EI-05 | L'application est pleinement utilisable sans jamais ouvrir l'écran Réglages. | O |
| EI-06 | Langue : français par défaut, structure prête pour l'anglais. | I |
| EI-07 | Les écrans sont utilisables au clavier et lisibles à 125 % et 150 % de mise à l'échelle. | S |

---

## 6. Exigences non fonctionnelles

| Réf. | Exigence | Cible |
|---|---|---|
| ENF-01 | Taille de l'exécutable | ≤ 30 Mo par plateforme |
| ENF-02 | Empreinte du runtime Borg téléchargé | ≤ 100 Mo sur disque |
| ENF-03 | Démarrage de l'interface | < 2 s sur matériel courant |
| ENF-04 | Latence d'interface | aucune opération bloquante > 100 ms |
| ENF-05 | Sauvegarde incrémentale sur 50 Go, peu de changements | < 5 min |
| ENF-06 | Plateformes cibles | Windows 10 21H2+ et 11 (x86-64) ; Ubuntu 22.04+, Debian 12+, Fedora 40+ (x86-64) |
| ENF-07 | Consommation mémoire de l'interface au repos | < 150 Mo |
| ENF-08 | Fonctionnement sans droits administrateur | intégral |
| ENF-09 | Fonctionnement hors ligne | l'interface s'ouvre et l'historique est consultable sans réseau |
| ENF-10 | Journalisation | journal local par exécution, rotation automatique, exportable en un clic pour le support |
| ENF-11 | Compatibilité ascendante des dépôts | tout dépôt créé par l'application reste lisible par `borg` 1.4 officiel sur une machine tierce |

---

## 7. Architecture imposée

### 7.1 Découpage

```
Interface (Fyne)  — n'appelle jamais Borg directement
        │
Cœur applicatif
   ├─ ConfigStore    (TOML)
   ├─ SecretStore    (trousseau OS, optionnel selon le mode)
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

### 7.2 Contraintes d'architecture

| Réf. | Contrainte |
|---|---|
| AR-01 | Toute exécution de Borg passe par l'interface `BorgRunner`. Aucun appel direct depuis l'interface graphique. |
| AR-02 | La traduction des chemins est de la responsabilité exclusive du `Runner`, jamais de l'interface ni de la configuration. |
| AR-03 | Un exécutable unique, avec les modes : interface (défaut), `--run <profil>`, `--check <profil>`, `--install-schedule <profil>`, `--print-passphrase <profil>`. |
| AR-04 | Aucun démon ni service résident. La sauvegarde planifiée est un processus court lancé par l'ordonnanceur du système. |
| AR-05 | Aucune dépendance à un binaire externe pour SSH et SFTP côté diagnostic : `golang.org/x/crypto/ssh` et `github.com/pkg/sftp`. |
| AR-06 | Le fichier de configuration reste lisible et éditable à la main : `~/.config/borgui/config.toml`, `%APPDATA%\borgui\config.toml`. |

---

## 8. Sécurité

| Réf. | Exigence |
|---|---|
| SEC-01 | Le runtime téléchargé est vérifié par empreinte SHA-256 compilée dans le binaire avant toute exécution. |
| SEC-02 | L'exécutable de l'application est signé (signature de code Windows). Sans signature, SmartScreen et les antivirus bloquent un binaire qui télécharge puis exécute un runtime contenant `python.exe` et `cygwin1.dll`. |
| SEC-03 | Clé SSH dédiée à l'application, `ed25519`, permissions restreintes, jamais partagée avec la clé personnelle de l'utilisateur. |
| SEC-04 | Épinglage de l'empreinte du serveur, `StrictHostKeyChecking=yes`, `known_hosts` propre à l'application. |
| SEC-05 | Passphrase transmise par `BORG_PASSCOMMAND`, jamais dans l'environnement ni dans une ligne de commande. |
| SEC-06 | Aucun secret dans les journaux, ni dans les fichiers d'état publiés, ni dans les configurations exportées. |
| SEC-07 | Un sous-compte Hetzner par poste, cloisonné, avec sa propre clé : un poste compromis ne peut ni lire ni détruire les sauvegardes des autres. |
| SEC-08 | Le mode non chiffré fait l'objet d'un énoncé explicite du risque au moment du choix : données lisibles par l'hébergeur et par tout détenteur de la clé SSH. Si les sauvegardes contiennent des données personnelles de tiers, la décision relève du RGPD. |
| SEC-09 | Activation du mode append-only côté dépôt proposée en option, avec explication de sa portée réelle : un client restreint peut marquer des archives comme supprimées, la suppression effective exige une opération depuis un client non restreint. |

---

## 9. Livrables

| Réf. | Livrable |
|---|---|
| LI-01 | Code source complet, dépôt Git, licence à arbitrer (voir §12) |
| LI-02 | Exécutable Windows x86-64 signé |
| LI-03 | Exécutable Linux x86-64, plus paquet `.deb` et AppImage |
| LI-04 | Chaîne d'intégration continue produisant les artefacts, avec exécuteurs Windows et Linux (Fyne requiert CGO) |
| LI-05 | Recette de construction du runtime Cygwin, reproductible, avec versions épinglées, et publication de l'archive et de son empreinte |
| LI-06 | Documentation utilisateur : installation, première configuration, restauration, voie hors ligne |
| LI-07 | **Procédure de restauration de secours** : comment restaurer un dépôt avec le `borg` officiel depuis une machine Linux, sans l'application, dans les deux modes de chiffrement |
| LI-08 | Jeu de tests automatisés, dont les tests de recette du §11 |

---

## 10. Jalons

| Jalon | Contenu | Condition de passage |
|---|---|---|
| **v0.1** | Programme en ligne de commande, sans interface : téléchargement et vérification du runtime, connexion à une vraie Storage Box, `init` dans les deux modes, `create` sur 50 Go réels, `list`. Puis restauration de l'archive Windows par le `borg` officiel d'une machine Linux. | TR-01 à TR-04 passants |
| **v0.2** | MVP : écrans État, Sauvegarde, Destination ; assistant complet ; sauvegarde manuelle avec progression ; historique local ; espaces réservés cloud ; voie hors ligne. | Une sauvegarde configurée de bout en bout par un utilisateur non technique |
| **v0.3** | Planification, fichier d'état SFTP du poste dans son sous-compte, export/import de configuration, notifications. | Trois postes, dont au moins un de chaque système, sauvegardant automatiquement vers des sous-comptes distincts |
| **v1.0** | Restauration guidée complète, test de restauration mensuel, `borg check` planifié, traduction des erreurs, documentation. | Recette du §11 intégralement passante |

Charge indicative pour un développeur seul à l'aise en Go : v0.1 quelques jours, v0.2 trois semaines, v0.3 trois semaines, v1.0 environ trois mois au total. La restauration et le traitement des cas d'erreur représentent à eux seuls près d'un tiers de la charge.

**La v0.1 est réalisée en premier, avant toute interface**, parce qu'elle porte le risque principal du projet : la viabilité du runtime Borg sous Windows et l'interopérabilité des archives.

---

## 11. Recette

Chaque test est binaire, exécuté sur les deux plateformes sauf mention contraire.

### 11.1 Interopérabilité et non-captivité

| Réf. | Test |
|---|---|
| TR-01 | Une archive créée sous Windows est listée et extraite sans erreur par `borg` 1.4 officiel sur une machine Linux, en mode chiffré. |
| TR-02 | Idem en mode non chiffré. |
| TR-03 | Les chemins de l'archive Windows contiennent bien la lettre de lecteur en première composante, sans préfixe `cygdrive`. |
| TR-04 | Un fichier restauré est identique bit pour bit à l'original (comparaison d'empreintes). |

### 11.2 Non-interactivité

| Réf. | Test |
|---|---|
| TR-10 | Une sauvegarde planifiée sur dépôt **non chiffré** s'exécute jusqu'au bout sans aucune interaction, sur un dépôt jamais vu par la machine. |
| TR-11 | Une sauvegarde planifiée sur dépôt chiffré s'exécute sans invite de saisie, la passphrase étant lue depuis le trousseau. |
| TR-12 | Aucun processus Borg ne reste bloqué en attente d'entrée après 100 exécutions planifiées consécutives. |

### 11.3 Installation et runtime

| Réf. | Test |
|---|---|
| TR-20 | Premier lancement sur une machine Windows vierge : téléchargement, vérification, installation et sauvegarde réussie sans droits administrateur. |
| TR-21 | Un runtime dont l'empreinte a été altérée est rejeté et l'installation interrompue avec message explicite. |
| TR-22 | Sans accès réseau, la voie hors ligne permet d'aboutir à une installation fonctionnelle. |
| TR-23 | Le téléchargement interrompu à mi-parcours est repris ou relancé proprement, sans laisser d'installation partielle. |

### 11.4 Robustesse

| Réf. | Test |
|---|---|
| TR-30 | Coupure réseau en pleine sauvegarde : l'échec est signalé clairement, le dépôt reste utilisable, la sauvegarde suivante aboutit. |
| TR-31 | Arrêt brutal du poste pendant une sauvegarde : le dépôt verrouillé est détecté et débloqué depuis l'interface, la sauvegarde suivante aboutit. |
| TR-32 | Poste éteint à l'heure planifiée : la sauvegarde se déclenche au démarrage suivant. |
| TR-33 | Chemins comportant accents, espaces, et dépassant 260 caractères : sauvegardés et restaurés correctement. |
| TR-34 | Un dossier OneDrive avec des fichiers en espace réservé est sauvegardé sans déclencher leur téléchargement, et l'exclusion est signalée à l'utilisateur. |
| TR-35 | Un fichier verrouillé provoque un code de retour 1 : l'exécution est présentée comme « terminée avec avertissements », la liste des fichiers ignorés est consultable, et la sauvegarde est exploitable. |

### 11.5 Interface et ergonomie

| Réf. | Test |
|---|---|
| TR-40 | Un utilisateur non technique, sans documentation, aboutit à une première sauvegarde planifiée en moins de dix minutes. |
| TR-41 | Une restauration d'un fichier unique, retrouvé par recherche, est réalisée sans assistance. |
| TR-42 | Aucun terme du vocabulaire Borg n'apparaît dans les écrans (vérification par revue exhaustive des chaînes de l'interface). |
| TR-43 | Le mode de chiffrement d'un dépôt existant est détecté automatiquement, sans question posée à l'utilisateur. |
| TR-44 | En mode chiffré, il est impossible d'atteindre la première sauvegarde sans avoir confirmé l'export de la clé de secours. |

### 11.6 Parc

| Réf. | Test |
|---|---|
| TR-50 | ~~Trois postes sauvegardent vers des sous-comptes distincts, et chacun affiche l'état des trois.~~ **Retirée** le 24 septembre 2026 (EF-84). |
| TR-51 | ~~Un poste n'ayant pas sauvegardé depuis 72 heures apparaît en orange sur les autres postes.~~ **Retirée** le 24 septembre 2026 (EF-84). |
| TR-52 | Une configuration exportée puis importée sur un autre poste aboutit à une sauvegarde fonctionnelle sans ressaisie autre que le sous-compte et la passphrase. |

---

## 12. Points à arbitrer avant démarrage

| Réf. | Question | Impact |
|---|---|---|
| PA-01 | Licence du code (logiciel libre ou distribution fermée) | Aucune contrainte du côté de Fyne (BSD-3) ni de Go, mais détermine la stratégie de distribution et d'hébergement du runtime |
| PA-02 | Budget pour un certificat de signature de code Windows | Sans signature, friction majeure au déploiement (SEC-02) |
| PA-03 | Hébergement du runtime Cygwin et de son empreinte | GitHub Releases suffit ; à confirmer si la distribution est fermée |
| PA-04 | Mode de chiffrement recommandé par défaut selon la nature des données | Détermine le libellé par défaut de l'écran EF-31, et l'exposition RGPD |
| PA-05 | Anglais dès la v1 ou plus tard | Charge d'internationalisation |
| PA-06 | Distribution Linux : `.deb` et AppImage suffisent, ou Flatpak attendu | Flatpak complique l'accès aux dossiers utilisateur et la planification systemd |

---

## 13. Glossaire

| Terme | Définition |
|---|---|
| **Archive** | Un instantané daté, tel que stocké par Borg. Appelé « sauvegarde » dans l'interface. |
| **Dépôt** | L'espace de stockage dédupliqué qui contient toutes les archives d'un poste. Appelé « destination » dans l'interface. |
| **Déduplication** | Mécanisme par lequel un bloc de données identique n'est stocké qu'une fois, quel que soit le nombre d'archives qui le référencent. |
| **`prune`** | Suppression des archives selon la politique de rétention. Appelé « conservation » dans l'interface. |
| **`compact`** | Libération effective de l'espace après suppression d'archives. |
| **Storage Box** | Service de stockage Hetzner accessible en SSH, avec support natif de BorgBackup sur le port 23. |
| **Sous-compte** | Utilisateur secondaire d'une Storage Box, cloisonné dans un sous-répertoire, avec son propre `authorized_keys`. |
| **Mode append-only** | Configuration limitant un client à l'ajout d'archives. Protection partielle : la suppression reste marquable côté client. |
| **Espace réservé cloud** | Fichier OneDrive ou Dropbox non téléchargé localement, matérialisé par un point d'analyse. |
| **Runtime Borg** | Sous Windows, l'ensemble Borg + Python + OpenSSH + bibliothèques Cygwin téléchargé par l'application. |
