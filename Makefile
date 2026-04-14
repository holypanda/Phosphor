BINARY := phosphor
LDFLAGS := -ldflags "-s -w"
DIST := dist

PLATFORMS := \
	linux/amd64 \
	linux/arm64 \
	windows/amd64 \
	darwin/amd64 \
	darwin/arm64

.PHONY: all clean dev

all: $(PLATFORMS)

$(PLATFORMS):
	@mkdir -p $(DIST)
	$(eval GOOS := $(word 1,$(subst /, ,$@)))
	$(eval GOARCH := $(word 2,$(subst /, ,$@)))
	$(eval EXT := $(if $(filter windows,$(GOOS)),.exe,))
	GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(LDFLAGS) -o $(DIST)/$(BINARY)-$(GOOS)-$(GOARCH)$(EXT) .

dev:
	go run . -dev

clean:
	rm -rf $(DIST)
