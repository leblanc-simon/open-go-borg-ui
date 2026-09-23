// Package borgruntime installe le moteur de sauvegarde sous Windows.
//
// L'exécutable de l'application n'embarque pas Borg : il récupère au premier
// lancement un runtime Cygwin autonome (Borg officiel, Python, OpenSSH), le
// vérifie, puis l'installe dans un dossier versionné (EF-02 à EF-06).
//
// La vérification n'est pas négociable : l'application s'apprête à exécuter ce
// code avec les droits de l'utilisateur et l'accès à ses données. L'empreinte
// attendue est compilée dans le binaire, jamais téléchargée (SEC-01).
package borgruntime

// Spec épingle le runtime associé à cette version de l'application.
//
// La version de Borg est figée par version de l'application : aucune mise à
// jour silencieuse du moteur, un changement de comportement de Borg au milieu
// de la vie d'un dépôt étant exactement ce qu'un logiciel de sauvegarde doit
// éviter (EF-09).
type Spec struct {
	// Version identifie le runtime et nomme son dossier d'installation.
	Version string
	// BorgVersion est la version que « borg --version » doit rapporter pour
	// que l'installation soit déclarée réussie (EF-05).
	BorgVersion string
	// URL est l'adresse HTTPS versionnée de l'archive.
	URL string
	// SHA256 est l'empreinte hexadécimale de l'archive.
	SHA256 string
	// Size est la taille attendue en octets, 0 si inconnue. Elle sert à
	// afficher une progression avant même la réponse du serveur.
	Size int64
}

// Pinned est le runtime de cette version de l'application.
//
// SHA256 et Size désignent l'archive produite par la recette de construction
// du runtime (LI-05, build/runtime). URL reste à renseigner une fois l'archive
// publiée : tant qu'elle est vide, seule la voie hors ligne est utilisable, ce
// que l'application signale explicitement.
var Pinned = Spec{
	Version:     "1.4.5-cygwin.1",
	BorgVersion: "1.4.5",
	URL:         "",
	SHA256:      "1926b2941925e2ccfd155eeb66a4067d93401d7248483cf0f5212d66371e60f7",
	Size:        62517029,
}

// Publié indique si le runtime épinglé peut être téléchargé.
func (s Spec) Published() bool { return s.URL != "" && s.SHA256 != "" }

// ArchiveName est le nom de l'archive du runtime, tel que la recette de
// construction le produit.
func (s Spec) ArchiveName() string { return "borgui-runtime-" + s.Version + ".tar.gz" }
