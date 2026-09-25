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
#   make test | vet | version | clean

MODULE  := leblanc.io/open-go-borg-ui
COMMIT  := $(shell git rev-parse --short=12 HEAD 2>/dev/null)
TAG     := $(shell git describe --tags --exact-match HEAD 2>/dev/null)
DIRTY   := $(shell git status --porcelain --untracked-files=no 2>/dev/null | grep -q . && echo -dirty)
VERSION ?= $(or $(TAG),$(COMMIT),dev)$(DIRTY)

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

.PHONY: build build-nogui build-windows check-size test vet version clean

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

build-windows:
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build $(GOFLAGS) -tags nogui -o $(DIST)/borgui-nogui.exe ./cmd/borgui

test:
	go test ./...

vet:
	go vet ./...

version:
	@echo $(VERSION)

clean:
	rm -rf $(DIST)/borgui $(DIST)/borgui-nogui $(DIST)/borgui-nogui.exe
