# Décisions arrêtées — v2

Périmètre validé : **Windows et Linux à parité**, **données utilisateur uniquement**, **quelques postes en configuration manuelle**.

Choix techniques arrêtés :

| Décision | Choix |
|---|---|
| Chiffrement Borg | **Facultatif**, choisi à la création du dépôt |
| Runtime Borg Windows | **Téléchargé au premier lancement**, non embarqué |
| Pile UI | **Go + Fyne** |
| Restauration | **Dans la v1**, pas reportée |
| Moteur Windows | Runtime Cygwin (code Borg officiel, non patché) |
| Version Borg | 1.4.x |

---

## 1. Ce que le périmètre élimine

VSS et clichés instantanés, ACL NTFS, fork `borg-windows`, WSL2, droits administrateur, planification en tant que SYSTEM, gestion centralisée de flotte. Justifications détaillées dans le rapport principal ; ces points sortent de la feuille de route.

Réserve unique : Outlook et quelques bases applicatives restent des fichiers verrouillés même dans un périmètre « données utilisateur » (voir §6).

---

## 2. Chiffrement facultatif

### 2.1 Deux modes exposés, et un seul écran pour choisir

| Libellé dans l'interface | Mode Borg | Passphrase | Conséquences |
|---|---|---|---|
| **Chiffré** (proposé par défaut) | `repokey-blake2` | Requise | Hetzner ne peut pas lire les données. Perte de la passphrase **et** de la clé exportée = données définitivement perdues. |
| **Non chiffré** | `none` | Aucune | Sauvegarde et restauration sans secret à gérer. Hetzner, et quiconque obtient la clé SSH, peut lire l'intégralité des données. |

Les modes intermédiaires de Borg (`authenticated`, `authenticated-blake2`, qui authentifient sans chiffrer) sont volontairement **non exposés** : ils exigent malgré tout une passphrase, ce qui annule l'intérêt du mode « sans secret » tout en compliquant l'interface.

### 2.2 Le point critique : le choix est irréversible

**Le mode de chiffrement est figé à `borg init` et ne peut pas être modifié ensuite.** Changer d'avis impose de créer un nouveau dépôt et de tout re-sauvegarder — soit, sur une Storage Box, plusieurs heures et la perte de tout l'historique.

Conséquences pour l'interface :

- Le choix apparaît **une seule fois**, dans l'assistant, sur un écran dédié, avec les deux conséquences énoncées en français simple. Pas de case à cocher noyée dans un écran de réglages.
- Il est **grisé et non modifiable** partout ailleurs dans l'application, avec la mention du mode en cours.
- Pour un dépôt **existant**, ne posez jamais la question : lisez `encryption.mode` dans la sortie de `borg info --json` et adaptez l'interface. Un utilisateur qui reconnecte un poste ne doit pas pouvoir se tromper.

### 2.3 Ce que le mode change dans le code

Le mode de chiffrement traverse presque toutes les couches. À traiter comme un paramètre de premier plan, pas comme une option :

- **SecretStore** devient optionnel. En mode `none`, aucune entrée dans le trousseau, `BORG_PASSCOMMAND` n'est pas défini.
- **`BORG_UNKNOWN_UNENCRYPTED_REPO_ACCESS_IS_OK=yes` est obligatoire** en mode `none`. Sans cette variable, Borg pose une question interactive au premier accès à un dépôt non chiffré, et **toute sauvegarde planifiée reste bloquée indéfiniment**. C'est le piège le plus coûteux de cette fonctionnalité. Ajoutez aussi `BORG_RELOCATED_REPO_ACCESS_IS_OK=yes` si vous autorisez le déplacement d'un dépôt.
- **L'export de clé de secours** (`borg key export --paper`) n'existe pas en mode `none` : il n'y a pas de clé. L'étape obligatoire de l'assistant devient conditionnelle. En mode chiffré, elle reste strictement obligatoire et non contournable.
- **L'écran État** doit afficher le mode du dépôt. Sur un parc mixte, savoir quels postes sont chiffrés est une information d'exploitation, pas un détail.
- **`borg check`** fonctionne dans les deux modes : Borg conserve des sommes de contrôle même sans chiffrement. En revanche, en mode `none`, une altération **malveillante** du dépôt n'est pas détectable — seule la corruption accidentelle l'est.

### 2.4 Ce qu'il faut dire à l'utilisateur, une fois, sans insister

Le mode non chiffré supprime le principal risque de perte de données du projet : la passphrase égarée. C'est un gain réel de robustesse. En échange, les données sont lisibles par l'hébergeur et par quiconque obtient la clé SSH du poste. Si les sauvegardes contiennent des données personnelles concernant des tiers, ce point relève du RGPD et mérite une décision explicite plutôt qu'un choix par défaut.

L'interface énonce ce compromis à l'écran de choix. Elle ne le répète pas ensuite.

---

## 3. Runtime Windows téléchargé au premier lancement

L'exécutable Windows reste autour de **25 Mo**. Le runtime Borg (Borg 1.4.5, Python, OpenSSH, runtime Cygwin, ~80 Mo décompressés) est récupéré au premier lancement.

### 3.1 Chaîne de récupération

1. Téléchargement HTTPS depuis une URL **versionnée et stable** (GitHub Releases convient).
2. **Vérification SHA-256 contre une empreinte compilée dans le binaire.** Non négociable : l'application va exécuter ce code avec les droits de l'utilisateur et l'accès à ses données.
3. Extraction dans un répertoire temporaire, puis renommage atomique vers `%LOCALAPPDATA%\borgui\runtime\<version>\`.
4. Vérification fonctionnelle : `borg --version` doit répondre la version attendue avant de déclarer l'installation réussie.

Le répertoire **versionné** permet une mise à jour sans casser une sauvegarde en cours et un retour arrière si la nouvelle version pose problème.

### 3.2 Les trois frictions à traiter dès la v0.2

- **Pas de réseau, ou proxy d'entreprise.** L'assistant doit échouer proprement, expliquer ce qui manque, respecter les variables de proxy système, et proposer une **voie hors ligne** : un ZIP téléchargeable manuellement à déposer dans un dossier indiqué, que l'application détecte et vérifie de la même façon. Sur un parc de quelques postes, c'est le mode d'installation qui sera réellement utilisé plus souvent que prévu.
- **Antivirus et SmartScreen.** Un exécutable non signé qui télécharge une archive contenant `python.exe` et `cygwin1.dll`, puis l'exécute, sera signalé par Defender ou bloqué par SmartScreen. Deux mesures : **signer l'exécutable** de l'application (certificat de signature de code, quelques centaines d'euros par an), et documenter l'exclusion antivirus du répertoire de runtime. À anticiper : découvert au moment du déploiement, ce point coûte plusieurs jours.
- **Politique de mise à jour du moteur.** La version de Borg est **épinglée par version de l'application**. Jamais de mise à jour silencieuse du moteur : un changement de comportement de Borg au milieu de la vie d'un dépôt est exactement ce qu'on ne veut pas dans un logiciel de sauvegarde. Proposez un bouton explicite « Mettre à jour le moteur de sauvegarde », avec la version cible affichée.

### 3.3 Placement dans l'assistant

Le téléchargement se fait **avant** le test de connexion, avec une barre de progression, et il est reprenable après un échec. Un premier lancement qui échoue au téléchargement sans explication claire est un abandon garanti.

---

## 4. Pile technique : Go + Fyne, confirmé

- Fyne série **2.7.x**, Go **1.22+** minimum. Wayland pris en charge par défaut.
- **Fyne dépend de CGO** : prévoyez une CI avec deux exécuteurs, `windows-latest` et `ubuntu-latest`. C'est plus simple et plus fiable que la compilation croisée via `fyne-cross`.
- Bibliothèques suffisantes pour l'ensemble du projet, sans dépendance externe au binaire : `os/exec` et `encoding/json` pour piloter Borg, `golang.org/x/crypto/ssh` et `github.com/pkg/sftp` pour le test de connexion et le tableau de bord, `github.com/zalando/go-keyring` pour la passphrase en mode chiffré, `modernc.org/sqlite` (pur Go, pas de CGO supplémentaire) pour l'historique et le cache de navigation.
- Icône de barre système en v0.3 : `fyne.io/systray`, intégré à Fyne.

Un seul exécutable, deux modes : `borgui` lance l'interface, `borgui --run <profil>` exécute la sauvegarde sans interface pour la tâche planifiée. La sauvegarde fonctionne donc même si l'interface n'est jamais ouverte.

---

## 5. Restauration en v1

### 5.1 La contrainte structurante : pas de `borg mount` sous Windows

FUSE n'existe pas sous Windows, et le runtime Cygwin n'en fournit pas d'équivalent. L'approche « monter l'archive comme un lecteur et laisser l'utilisateur parcourir avec l'explorateur » est donc **exclue**.

La restauration se construit sur deux commandes :
- **`borg list --json <archive>`** pour la navigation ;
- **`borg extract`** pour l'extraction, avec la convention de chemins du §7.

Sous Linux, `borg mount` reste disponible et pourrait offrir une expérience plus fluide. **Ne construisez pas la fonctionnalité dessus** : développez le chemin `list` + `extract`, identique sur les deux plateformes, et traitez `mount` comme un raccourci Linux optionnel s'il apporte quelque chose.

### 5.2 Navigation : le cache est obligatoire

`borg list --json` sur une archive de plusieurs centaines de milliers de fichiers prend du temps et transfère un volume de métadonnées non négligeable. Au premier accès à une archive, stockez la liste dans SQLite, indexée par identifiant d'archive. Les archives étant immuables, ce cache n'est **jamais** à invalider — il ne peut que grandir. La deuxième ouverture est alors instantanée.

Navigation par arborescence, avec recherche par nom de fichier : dans la pratique, l'utilisateur qui restaure cherche un fichier dont il connaît approximativement le nom, il ne parcourt pas les dossiers.

### 5.3 Extraction : le comportement par défaut doit être le plus sûr

- **Destination par défaut : un dossier neuf** (`Restauration <date>` sur le Bureau), pas l'emplacement d'origine. Une restauration ne doit jamais écraser silencieusement du travail plus récent.
- Restauration à l'emplacement d'origine possible, mais derrière une confirmation explicite énonçant ce qui sera écrasé.
- Sous Windows, extraction avec le répertoire courant positionné sur la racine du lecteur et `--strip-components 1`, conformément à la convention du §7.
- Une **restauration partielle** (un dossier, un fichier) doit être aussi accessible qu'une restauration complète : c'est le cas d'usage réel dans plus de neuf situations sur dix.
- `borg export-tar` en option « tout récupérer dans un seul fichier », utile pour une migration.

### 5.4 Le test de restauration automatique

Une fois par mois, l'application extrait un fichier tiré au hasard dans un répertoire temporaire et compare son contenu à l'original s'il existe encore. Le résultat apparaît sur l'écran État : « Restauration vérifiée le 2 septembre ».

Cette fonctionnalité coûte peu et constitue le seul élément qui distingue une sauvegarde présumée fonctionnelle d'une sauvegarde vérifiée. Elle appartient à la v1, au même titre que la restauration manuelle.

---

## 6. Pièges spécifiques « données utilisateur sous Windows »

1. **OneDrive et Dropbox « fichiers à la demande ».** Les fichiers non téléchargés sont des points d'analyse ; les inclure déclenche leur hydratation complète, donc le téléchargement de la totalité du contenu cloud. Détectez l'attribut `FILE_ATTRIBUTE_RECALL_ON_DATA_ACCESS` et **ignorez ces fichiers par défaut**, en le signalant dans l'interface. À traiter en v0.2.
2. **Outlook (`.ost` / `.pst`).** Verrouillés si Outlook tourne, volumineux, mal dédupliqués. Exclure `.ost` par défaut (cache reconstructible), avertir explicitement pour `.pst`. Seul cas où l'absence de VSS se fait sentir dans ce périmètre.
3. **`AppData`.** Mélange de données irremplaçables et de dizaines de gigaoctets de caches. Proposez un préréglage « AppData\Roaming sauf caches connus » plutôt qu'un choix tout ou rien.
4. **Encodage et chemins longs.** Accents et espaces dans les chemins via Cygwin, chemins de plus de 260 caractères, jonctions et liens symboliques : à tester dès la v0.1.

---

## 7. Convention de chemins Windows

À figer avant la première sauvegarde en production : elle est très coûteuse à changer ensuite.

`borg create` s'exécute avec le répertoire courant positionné sur `/cygdrive`, et reçoit des chemins **relatifs** préfixés par la lettre de lecteur.

L'utilisateur sélectionne `C:\Users\marc\Documents` et `D:\Projets`. L'application exécute, depuis `/cygdrive` :

```
borg create ... ::'{hostname}-{now}' c/Users/marc/Documents d/Projets
```

L'archive contient `c/Users/marc/Documents/...` et `d/Projets/...`. La lettre de lecteur est préservée (multi-disque non ambigu), aucun préfixe `cygdrive` ne pollue l'archive, et la restauration se fait par `cd C:\` puis `borg extract ... c --strip-components 1`.

Sous Linux, chemins absolus habituels. Les archives n'ont donc pas la même forme selon l'OS : sans conséquence, puisqu'il y a un dépôt par poste.

**Exclusions** : toujours stockées comme motifs portables (`**/node_modules`, `**/*.iso`), jamais comme chemins absolus. Elles fonctionnent alors à l'identique sur les deux plateformes.

---

## 8. Modèle multi-postes

**Un sous-compte Hetzner et un dépôt par poste.** Chaque sous-compte a son répertoire et son propre `authorized_keys`, donc un poste compromis ne peut ni lire ni détruire les sauvegardes des autres. Ne mutualisez pas un dépôt entre postes : Borg gère mal les accès concurrents, et le secret partagé donnerait à chaque poste un accès total aux données des autres.

**Portabilité** : un bouton « Exporter la configuration » produisant un TOML sans secrets, et « Importer » sur le poste suivant, qui ne redemande que le sous-compte et, en mode chiffré, la passphrase.

**Tableau de bord partagé** (recommandé, faible coût) : à la fin de chaque exécution, le poste dépose en SFTP `status/<hostname>.json` contenant date, statut, durée, volume, taille du dépôt, mode de chiffrement et prochaine exécution. L'écran État agrège ces fichiers et affiche l'état de **tous** les postes. Aucune infrastructure supplémentaire — `github.com/pkg/sftp` suffit, sans binaire externe.

Un poste dont le fichier d'état n'a pas été mis à jour depuis plus de 48 h passe en orange. **L'absence de sauvegarde est le mode de défaillance le plus fréquent, et le seul qu'un affichage purement local ne peut pas détecter.**

---

## 9. Feuille de route

**v0.1 — Le test qui décide de tout, sans interface**

Un binaire Go en ligne de commande qui, sur une machine Windows vierge : télécharge et vérifie le runtime, se connecte à une vraie Storage Box sur le port 23, exécute `borg init` **dans les deux modes de chiffrement**, `borg create` sur 20 Go de données réelles avec la convention du §7, puis `borg list`.

Puis, **sur une machine Linux**, restaurer une archive créée sous Windows avec le `borg` du paquet distribution — dans les deux modes. Si cette étape échoue, tout le projet est à revoir, d'où sa place en premier.

Mesurez la durée d'une sauvegarde initiale et d'une incrémentale, et vérifiez qu'une sauvegarde non chiffrée s'exécute **sans aucune interaction** (`BORG_UNKNOWN_UNENCRYPTED_REPO_ACCESS_IS_OK`).

**v0.2 — MVP**

Écrans État / Sauvegarde / Destination, assistant de premier lancement avec téléchargement du runtime et choix du mode de chiffrement, sauvegarde manuelle avec progression, historique local, gestion des espaces réservés OneDrive, voie d'installation hors ligne.

**v0.3 — Automatisation et parc**

Planification (`schtasks`, timers systemd utilisateur avec `Persistent=true`), fichier d'état SFTP et tableau de bord multi-postes, export/import de configuration, notifications, icône de barre système.

**v1.0 — Confiance**

Restauration guidée (navigation, recherche, extraction partielle, cache SQLite), test de restauration automatique mensuel, `borg check` planifié, export obligatoire de la clé en mode chiffré, traduction des erreurs Borg en français.

**Hors feuille de route** : VSS, ACL NTFS, profils multiples par poste, destinations multiples, hooks pré/post.

La restauration représentant environ 30 % du travail d'interface, la v1.0 est nettement plus lourde que dans l'estimation initiale : comptez plutôt trois mois pour un développeur seul à l'aise en Go, dont un tiers sur la restauration et le traitement des cas d'erreur.

---

## 10. Points de vigilance résiduels

| Risque | Mitigation |
|---|---|
| Perte de la passphrase en mode chiffré | Export de clé obligatoire et non contournable dans l'assistant, rappel périodique tant qu'il n'est pas confirmé |
| Blocage indéfini d'une sauvegarde planifiée sur dépôt non chiffré | `BORG_UNKNOWN_UNENCRYPTED_REPO_ACCESS_IS_OK=yes` systématique, et test de non-interactivité en CI |
| Runtime non téléchargeable (proxy, réseau filtré) | Voie hors ligne documentée dès la v0.2 |
| Blocage antivirus / SmartScreen | Signature de code de l'exécutable, exclusion documentée |
| Dépôt verrouillé après un plantage | Détection de `LockTimeout` et bouton unique « Débloquer le dépôt » |
| `prune` mal configuré | `--list --dry-run` affiché avant toute confirmation de changement de rétention |
| Poste éteint à l'heure planifiée | `Persistent=true` sous systemd, rattrapage sous Windows, déclenchement au retour du réseau |
| Corruption d'un chunk dédupliqué | `borg check` mensuel, snapshots Hetzner activés, second dépôt pour les données vitales |
