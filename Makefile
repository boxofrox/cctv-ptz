.PHONY: all install build

VERSION := 0.1.0
BUILD_DATE := $(shell date -u +%Y%m%d.%H%M%S.%3N)
PKG := github.com/boxofrox/cctv-ptz

LDFLAGS = -ldflags "-X main.VERSION=$(VERSION) -X main.BUILD_DATE=$(BUILD_DATE)"

all: install

install:
	go install $(LDFLAGS) $(PKG)

build:
	go build $(LDFLAGS) $(PKG)
