# Construction d'OpenGoBorgUI.
#
# La version injectée dans l'exécutable est le tag du commit courant quand il
# en porte un (publication), sinon l'identifiant du commit (développement).
# Des modifications non commitées l'accompagnent de « -dirty ».
#
#   make build          interface et ligne de commande (CGO, bibliothèques graphiques)
#   make build-nogui    ligne de commande seule, sans CGO
#   make build-windows  ligne de commande Windows, compilée depuis Linux
#   make check-size     vérifie que l'exécutable publié tient sous 30 Mo
#   make deb            paquet .deb, depuis l'exécutable de « make build »
#   make build-windows-gui  exécutable Windows avec interface — sous Windows
#   make msi            paquet MSI, depuis dist/borgui.exe (wixl, sous Linux)
#   make test | vet | version | clean

MODULE  := leblanc.io/open-go-borg-ui
COMMIT  := $(shell git rev-parse --short=12 HEAD 2>/dev/null)
TAG     := $(shell git describe --tags --exact-match HEAD 2>/dev/null)
DIRTY   := $(shell git status --porcelain --untracked-files=no 2>/dev/null | grep -q . && echo -dirty)
VERSION ?= $(or $(TAG),$(COMMIT),dev)$(DIRTY)

# Les paquets ont leurs propres règles de version. Une publication porte le
# tag sans son « v » ; en développement :
#   .deb : 0.0~git<date>.<commit>, qui se classe sous toute vraie version ;
#   MSI  : trois nombres obligatoires, le troisième étant le nombre de
#          commits, pour qu'une construction plus récente remplace l'autre.
TAG_NUMBER  := $(patsubst v%,%,$(TAG))
COMMIT_DATE := $(shell git log -1 --format=%cd --date=format:%Y%m%d 2>/dev/null)
COMMITS     := $(shell git rev-list --count HEAD 2>/dev/null)
DEB_VERSION ?= $(or $(TAG_NUMBER),0.0~git$(COMMIT_DATE).$(COMMIT))
MSI_VERSION ?= $(or $(TAG_NUMBER),0.0.$(COMMITS))

DIST    := dist
# -s -w retirent les informations de débogage : l'exécutable publié doit
# rester sous 30 Mo (ENF). -trimpath retire les chemins de la machine de
# construction.
LDFLAGS := -s -w -X $(MODULE)/internal/version.Version=$(VERSION)
GOFLAGS := -trimpath -ldflags "$(LDFLAGS)"

# no_emoji écarte la police d'émojis que Fyne embarque par défaut (4,2 Mo) :
# l'interface n'en affiche aucun, et tous ses caractères sont couverts par
# Inter, la police principale. Sans elle, l'exécutable dépasse 30 Mo.
GUI_TAGS := no_emoji

# MAX_SIZE est le plafond de l'exécutable publié, en octets (30 Mo).
MAX_SIZE := 30000000

.PHONY: build build-nogui build-windows build-windows-gui winres check-size deb msi test vet version clean

build:
	go build $(GOFLAGS) -tags "$(GUI_TAGS)" -o $(DIST)/borgui ./cmd/borgui

check-size: build
	@size=$$(wc -c < $(DIST)/borgui); \
	if [ $$size -gt $(MAX_SIZE) ]; then \
		echo "$(DIST)/borgui : $$size octets, au-delà du plafond de $(MAX_SIZE)"; exit 1; \
	fi; \
	echo "$(DIST)/borgui : $$size octets, sous le plafond de $(MAX_SIZE)"

build-nogui:
	CGO_ENABLED=0 go build $(GOFLAGS) -tags nogui -o $(DIST)/borgui-nogui ./cmd/borgui

build-windows: winres
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build $(GOFLAGS) -tags nogui -o $(DIST)/borgui-nogui.exe ./cmd/borgui

# L'exécutable Windows avec interface se compile sous Windows : Fyne exige
# CGO et un compilateur C (MinGW-w64). -H=windowsgui évite une fenêtre de
# console derrière l'interface et à chaque sauvegarde planifiée ; la ligne
# de commande s'attache alors à la console qui l'a lancée.
build-windows-gui: winres
	CGO_ENABLED=1 go build -trimpath -ldflags "$(LDFLAGS) -H=windowsgui" -tags "$(GUI_TAGS)" -o $(DIST)/borgui.exe ./cmd/borgui

# winres intègre à l'exécutable Windows son icône, sa version et son
# manifeste : pas d'élévation demandée (ENF-08), mise à l'échelle par écran.
# Le fichier produit, propre à chaque version, n'est pas versionné.
winres:
	go run github.com/tc-hib/go-winres@v0.3.3 simply --arch amd64 --out cmd/borgui/rsrc \
		--manifest gui --icon packaging/icons/borgui.ico \
		--product-name OpenGoBorgUI --file-description OpenGoBorgUI \
		--original-filename borgui.exe --copyright "Simon Leblanc, WTFPL" \
		--product-version $(MSI_VERSION).0 --file-version $(MSI_VERSION).0

deb: build
	packaging/build-deb.sh $(DIST)/borgui $(DEB_VERSION) $(DIST)

msi:
	packaging/build-msi.sh $(DIST)/borgui.exe $(MSI_VERSION) $(DIST)

test:
	go test ./...

vet:
	go vet ./...

version:
	@echo $(VERSION)

clean:
	rm -rf $(DIST)/borgui $(DIST)/borgui-nogui $(DIST)/borgui-nogui.exe $(DIST)/*.deb $(DIST)/*.msi
