# tiny-samba build targets.
#
# Override REGISTRY to push elsewhere:
#   make image push REGISTRY=registry.lan/zot
#   make image push REGISTRY=ghcr.io/youruser
#
# Build on a remote Docker daemon (a Docker context) instead of locally — useful
# from a Mac dev box when the target deploy is Linux/amd64:
#   make image push BUILDER_CONTEXT=remote-builder
# The image is built and stored on the remote daemon; `push` uploads from there
# straight to the registry, no local pull/push round-trip.

REGISTRY ?= localhost
NAME     := tiny-samba
TAG      ?= dev
IMAGE    := $(REGISTRY)/$(NAME):$(TAG)

# Optional Docker context for image / push. Empty = active context (usually
# local). Pass `BUILDER_CONTEXT=remote-builder` to target a remote daemon.
BUILDER_CONTEXT ?=
DOCKER_BUILD := docker$(if $(BUILDER_CONTEXT), --context $(BUILDER_CONTEXT))

# Go-in-Docker for build/test/tidy. These use bind mounts of the source tree
# so they MUST run on a *local* Docker daemon — bind mounts on a remote
# context would resolve on the remote host's filesystem (where the source
# doesn't exist). We force the local context explicitly with --context so
# this works even when the user's active context is a remote builder.
#
# LOCAL_CONTEXT names your local Docker daemon's context. Defaults to
# "default" which works for plain Docker and most Docker Desktop setups. If
# yours is different (newer Docker Desktop sometimes uses "desktop-linux";
# Colima uses "colima"; etc.), override:  LOCAL_CONTEXT=desktop-linux make tidy
# Run `docker context ls` to see what contexts you have.
LOCAL_CONTEXT ?= default
GO_IMAGE      := golang:1.22-bookworm
GO_RUN        := docker --context $(LOCAL_CONTEXT) run --rm -v $(CURDIR):/src -w /src $(GO_IMAGE)

.PHONY: help build test tidy image push run-local clean

help:
	@echo "Targets:"
	@echo "  build      Build static linux/amd64 binary into ./tiny-samba (Go-in-Docker, local)."
	@echo "  test       Run go test ./... (Go-in-Docker, local)."
	@echo "  tidy       Run go mod tidy (Go-in-Docker, local)."
	@echo "  image      Build the OCI image $(IMAGE)."
	@echo "             Set BUILDER_CONTEXT=<name> to build on a remote Docker daemon."
	@echo "  push       Push $(IMAGE). Uses the same context as image."
	@echo "  run-local  Smoke test on the local Docker daemon (always builds locally)."
	@echo "  clean      Remove built artifacts."
	@echo ""
	@echo "Active build context: $(if $(BUILDER_CONTEXT),$(BUILDER_CONTEXT) (remote),local)"

build:
	$(GO_RUN) sh -c 'CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o tiny-samba ./cmd/tiny-samba'

test:
	$(GO_RUN) go test ./...

tidy:
	$(GO_RUN) go mod tidy

image:
	$(DOCKER_BUILD) build -t $(IMAGE) -f docker/Dockerfile .

push:
	$(DOCKER_BUILD) push $(IMAGE)

# Local smoke test. Always builds and runs against the *local* Docker daemon
# regardless of BUILDER_CONTEXT — bind mounts and host port 1445 are
# meaningful only on the local machine.
run-local:
	docker --context $(LOCAL_CONTEXT) build -t $(NAME):smoketest -f docker/Dockerfile .
	@mkdir -p /tmp/tiny-samba-data
	docker --context $(LOCAL_CONTEXT) run --rm -it \
		--cap-add=NET_BIND_SERVICE \
		-v $(CURDIR)/examples/config.yaml:/etc/tiny-samba/config.yaml:ro \
		-v $(CURDIR)/examples/passwords.yaml:/etc/tiny-samba/passwords.yaml:ro \
		-v /tmp/tiny-samba-data:/data \
		--tmpfs /var/lib/samba \
		-p 1445:445 \
		--name tiny-samba-smoketest \
		$(NAME):smoketest

clean:
	rm -f tiny-samba
