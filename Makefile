VERSION ?= 0.3.1
GOLANGCI_LINT_VERSION ?= 2.13.2
WAILS_VERSION ?= v2.16.0
# Passed to `wails build -platform` when set, e.g. darwin/universal.
DESKTOP_PLATFORM ?=

# The desktop application links the platform web view through cgo. Wails selects
# WebKitGTK 4.0 unless told otherwise; distributions that ship only 4.1 (Ubuntu
# 24.04 and later, Fedora 40 and later) need the webkit2_41 tag.
comma := ,
WEBKIT_TAG := $(shell pkg-config --exists webkit2gtk-4.1 2>/dev/null && echo webkit2_41)
DESKTOP_TAGS := desktop,production$(if $(WEBKIT_TAG),$(comma)$(WEBKIT_TAG))

.PHONY: build web test package clean lint fmt install-lint desktop desktop-dev desktop-test desktop-lint install-wails

build: web
	cd hub && go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o visual-tmux-client ./cmd/visual-tmux-client

web:
	npm --prefix hub/web ci
	npm --prefix hub/web run build

install-lint:
	./scripts/install-golangci-lint.sh

lint: web
	@which golangci-lint > /dev/null 2>&1 || { \
		echo "golangci-lint not found. Install version $(GOLANGCI_LINT_VERSION) via 'make install-lint' or ./scripts/install-golangci-lint.sh" >&2; \
		exit 1; \
	}
	cd hub && golangci-lint config verify --config ../.golangci.yml
	cd hub && golangci-lint run --config ../.golangci.yml ./...

fmt:
	@which golangci-lint > /dev/null 2>&1 || { \
		echo "golangci-lint not found. Install version $(GOLANGCI_LINT_VERSION) via 'make install-lint' or ./scripts/install-golangci-lint.sh" >&2; \
		exit 1; \
	}
	cd hub && golangci-lint fmt --config ../.golangci.yml

test: web
	npm --prefix hub/web test
	cd hub && go test ./...
	cd hub && go vet ./...

package:
	VERSION=$(VERSION) ./scripts/package.sh

# Desktop application (hub/desktop, a separate Go module so the server's build
# never needs a C toolchain or a web view). `make desktop` packages it with the
# Wails CLI: an .app bundle on macOS, a binary on Linux, under
# hub/desktop/build/bin.
install-wails:
	go install github.com/wailsapp/wails/v2/cmd/wails@$(WAILS_VERSION)

desktop: web
	@which wails > /dev/null 2>&1 || { \
		echo "wails not found. Install it with 'make install-wails'" >&2; \
		exit 1; \
	}
	cd hub/desktop && wails build -s -skipbindings -clean -trimpath \
		$(if $(DESKTOP_PLATFORM),-platform $(DESKTOP_PLATFORM)) \
		-tags "$(WEBKIT_TAG)" -ldflags "-X main.version=$(VERSION)"

# A plain go build of the desktop binary, without the Wails CLI or packaging.
desktop-dev: web
	cd hub/desktop && go build -tags $(DESKTOP_TAGS) -o build/bin/visual-tmux-client-desktop .

desktop-test: web
	cd hub/desktop && go test -tags $(DESKTOP_TAGS) ./...
	cd hub/desktop && go vet -tags $(DESKTOP_TAGS) ./...

desktop-lint: web
	@which golangci-lint > /dev/null 2>&1 || { \
		echo "golangci-lint not found. Install version $(GOLANGCI_LINT_VERSION) via 'make install-lint'" >&2; \
		exit 1; \
	}
	cd hub/desktop && golangci-lint run --config ../../.golangci.yml --build-tags $(DESKTOP_TAGS) ./...

clean:
	rm -rf hub/web/dist hub/visual-tmux-client hub/desktop/build/bin hub/desktop/build/wailsjs release
