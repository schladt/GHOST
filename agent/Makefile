GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
BASE_NAME=ghost_client
CLIENT_VERSION=0.1
OS=$(shell go env GOOS)
ARCH=$(shell go env GOARCH)
EXT=$(shell go env GOEXE)
BINARY_PATH=bin/$(BASE_NAME)_$(OS)_$(ARCH)$(EXT)

all: build final

build:
	$(GOBUILD) -v -ldflags="-s -w -X main.version=$(CLIENT_VERSION)" -o $(BINARY_PATH) ./

test: 
	$(GOTEST) -v ./...

clean:
	$(GOCLEAN)

run:
	build
	./$(BINARY_PATH)

final:
ifeq ($(OS),linux)
	$(eval OUTFILE := $(shell sha256sum $(BINARY_PATH)))
else ifeq ($(OS),darwin)
	$(eval OUTFILE := $(shell shasum -a 256 $(BINARY_PATH)))
else ifeq ($(OS),windows)
	$(eval OUTFILE := $(shell PowerShell.exe Get-FileHash $(BINARY_PATH)))
else
	$(error Unsupported: $(NATIVE))
endif
	@echo "Binary created : $(OUTFILE)"

deps:
	$(GOCMD) mod tidy
