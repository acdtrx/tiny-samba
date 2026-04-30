# syntax=docker/dockerfile:1

# --- build stage ---------------------------------------------------------
FROM golang:1.22-bookworm AS builder

WORKDIR /src

# Copy everything in one shot. We could split go.mod/go.sum into a separate
# layer for better caching, but `go.sum*` globbing breaks BuildKit when no
# go.sum exists yet, and conditional COPY isn't worth the complexity for
# what's still a small project.
COPY . .

# `go build` is strict about go.sum being complete. `go mod tidy` populates
# every required hash (`go mod download` is unreliable about this — see
# golang/go#42504). When go.sum is already in sync this is a no-op other
# than a metadata check. Tidy will only ADD missing entries here, not remove
# anything, because we just COPY-ed the source.
RUN go mod tidy

RUN CGO_ENABLED=0 go build -trimpath \
        -ldflags="-s -w -X main.version=$(cat VERSION 2>/dev/null || echo dev)" \
        -o /tiny-samba ./cmd/tiny-samba

# --- runtime stage -------------------------------------------------------
FROM debian:stable-slim

# samba: smbd, nmbd, smbcontrol, pdbedit, smbpasswd. samba-common-bin pulls in
# the userspace tools (pdbedit, smbpasswd). We don't install winbind / printers /
# AD bits — too much for this use case.
#
# wsdd2: WS-Discovery + LLMNR responder so the server appears in Windows
# Explorer's Network sidebar (modern Windows uses WSD, not the legacy NetBIOS
# browser). Small C daemon, no Python runtime. Disable via `server.wsd: false`
# in config if not wanted; the supervisor will skip launching it.
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        samba samba-common-bin \
        samba-vfs-modules \
        wsdd2 \
        ca-certificates \
        tini && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

# Layout: config files mounted at /etc/tiny-samba; data at /data;
# Samba runtime state at /var/lib/samba (made tmpfs in the wisp/docker run config).
RUN mkdir -p /etc/tiny-samba /etc/samba /data \
             /var/lib/samba/private /var/cache/samba /var/log/samba /run/samba && \
    chmod 0700 /var/lib/samba/private

COPY --from=builder /tiny-samba /usr/local/bin/tiny-samba

EXPOSE 445/tcp 3702/udp 5357/tcp

# tini reaps zombies and forwards signals — useful even though smbd is
# well-behaved, in case any helper subprocess outlives a request.
ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/tiny-samba", "serve"]
