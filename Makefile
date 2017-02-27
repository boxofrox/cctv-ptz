.PHONY: all install build

VERSION := 1.0.0
BUILD_DATE := $(shell date -u +%Y%m%d.%H%M%S.%3N)
PKG := github.com/boxofrox/cctv-ptz
BIN := cctv-ptz

LDFLAGS = -ldflags "-X main.VERSION=$(VERSION) -X main.BUILD_DATE=$(BUILD_DATE)"

# define BIN_EXT as env var to attach a file extension to the built executable
# example: BIN_EXT=.exe make build

all: install

install:
	go install -o "$(BIN)$(BIN_EXT)" $(LDFLAGS) $(PKG)

build:
	go build -o "$(BIN)$(BIN_EXT)" $(LDFLAGS) $(PKG)
