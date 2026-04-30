# Building tiny-samba

Everything you need to build, test, and ship the image. If something doesn't
work, scroll to **Common errors** at the bottom.

## Prerequisites

- **Docker** with at least one usable context (Docker Desktop, Colima, plain
  `dockerd` on Linux — anything works).
- **Optional**: a remote Linux Docker daemon configured as a context for fast
  native-arch image builds when you're on a Mac. Set it up once with:
  ```sh
  docker context create remote-builder --docker host=ssh://user@linuxhost
  ```
- You do **not** need Go installed locally. All Go invocations run inside a
  `golang:1.22-bookworm` container.

## The two contexts the Makefile knows about

| Variable | What it controls | Default |
|---|---|---|
| `LOCAL_CONTEXT` | Where `make tidy` / `test` / `build` / `run-local` run. Must be a daemon on this machine because they use bind mounts. | `default` |
| `BUILDER_CONTEXT` | Where `make image` / `push` run. Empty = use whatever your active context is. Set it to `remote-builder` (or whatever) to build on a remote daemon. | (empty) |

Run `docker context ls` to see what you've got. Note: Docker Desktop on newer
Macs sometimes uses `desktop-linux` as the local context name instead of
`default`. If `default` doesn't exist, override:

```sh
LOCAL_CONTEXT=desktop-linux make tidy
# or
export LOCAL_CONTEXT=desktop-linux  # add to your shell profile to persist
```

## Quick start

```sh
cd /path/to/tiny-samba

# 1. Generate go.sum (one-time, also after editing imports)
make tidy

# 2. Run unit tests
make test

# 3. Build the image locally
make image

# 4. Smoke test on this machine — binds to host:1445
make run-local
# In another terminal:
smbclient -L //localhost:1445 -U alice    # password: hunter2
```

## Building & pushing for deploy

For a production-bound image you usually want:
- A real registry (not `localhost`)
- A meaningful tag (not `dev`)
- Native Linux/amd64 build (not Mac→QEMU)

```sh
# All in one shot
make image push \
    REGISTRY=registry.example.com/acdtrx \
    TAG=latest \
    BUILDER_CONTEXT=remote-builder
```

This builds on `remote-builder` (a Linux daemon, native amd64), tags as
`registry.example.com/acdtrx/tiny-samba:latest`, and pushes from that
remote daemon directly to the registry — no local pull/re-push round trip.

### Registry login when using a remote builder

`docker push` runs on whatever daemon `BUILDER_CONTEXT` points to. So your
**registry credentials must live on that daemon**, not on your Mac:

```sh
# This logs in on the remote daemon (creds are stored in its config)
docker --context remote-builder login registry.example.com
```

If you skip this and try to push, you'll get a 401 / unauthorized error.

### Building locally instead (no remote builder)

If you don't have a remote builder, just leave `BUILDER_CONTEXT` unset:

```sh
make image push REGISTRY=registry.example.com/acdtrx TAG=latest
docker login registry.example.com    # log in on local Docker
```

On Apple Silicon Macs without a remote builder, this does an arm64 build
(or arm64-on-amd64 if you set `--platform linux/amd64`, with QEMU emulation
that is much slower). For deploy to amd64 Linux, the remote builder is
strongly recommended.

## All Makefile variables

| Var | Default | Effect |
|---|---|---|
| `REGISTRY` | `localhost` | Registry + path prefix. Joined as `$(REGISTRY)/tiny-samba:$(TAG)`. |
| `TAG` | `dev` | Image tag. |
| `BUILDER_CONTEXT` | (empty) | Docker context for `image` / `push`. Empty = active context. |
| `LOCAL_CONTEXT` | `default` | Docker context for everything that bind-mounts the source. Must be a local daemon. |

## All Makefile targets

| Target | What it does | Context used |
|---|---|---|
| `make tidy` | `go mod tidy` (generates / updates `go.sum`) | `LOCAL_CONTEXT` |
| `make test` | Run all unit tests | `LOCAL_CONTEXT` |
| `make build` | Build the static binary into `./tiny-samba` | `LOCAL_CONTEXT` |
| `make image` | Build the OCI image | `BUILDER_CONTEXT` (or active) |
| `make push` | Push the image to the registry | `BUILDER_CONTEXT` (or active) |
| `make run-local` | Build locally + run a smoke test container on `localhost:1445` | `LOCAL_CONTEXT` |
| `make clean` | Remove `./tiny-samba` | — |
| `make help` | Print the target list and the active build context | — |

## Common errors

### `go: go.mod file not found in current directory or any parent directory`

You ran `make tidy` (or `test` / `build`) and the bind mount didn't carry your
source into the container. Cause: the active Docker context is a remote
daemon and the path doesn't exist there.

**Fix**: set `LOCAL_CONTEXT` to your local daemon's context.

```sh
docker context ls       # find your local one
LOCAL_CONTEXT=default make tidy           # or
LOCAL_CONTEXT=desktop-linux make tidy
```

If you don't know which is local, the one with `unix://` or
`unix:///var/run/docker.sock` in the DOCKER ENDPOINT column is local. The
ones with `ssh://` or `tcp://` are remote.

### `missing go.sum entry for module providing package …`

`go.sum` is missing required entries. The Dockerfile runs `go mod tidy` to
populate it — if you still see this error, either:

1. You're on an old Dockerfile that uses `go mod download` instead of
   `go mod tidy` (download is unreliable about populating go.sum — see
   golang/go#42504). Pull the latest Dockerfile.
2. The `go mod tidy` step itself failed (network down, proxy blocked,
   private module without auth). Scroll up in the build output.

Generating `go.sum` locally once is the cleanest fix and makes future
builds skip the tidy roundtrip:

```sh
make tidy && git add go.sum
```

### `denied: requested access to the resource is denied` on push

You're not logged in to the registry **on the daemon doing the push**. With
the remote builder:

```sh
docker --context remote-builder login registry.example.com
```

With local builder, just `docker login registry.example.com`.

### `http2: server: error reading preface from client dummy-1: read |0: file already closed`

Cosmetic noise from BuildKit's HTTP/2 internals. Always shows up at the start
of `docker build`, never indicates a real problem. Ignore.

### `make image` failed but `make tidy` works

Something specific to the Docker build (not the Go side). Read the error in
the `RUN ...` step that failed. Most common: `apt-get install` failing
because of a network blip — re-run `make image`.

### Image built but the container exits immediately

Check container logs:

```sh
docker logs tiny-samba-smoketest    # for run-local
docker logs <your-container-name>   # otherwise
```

Most likely: invalid `config.yaml` or `passwords.yaml`. tiny-samba prints a
clear `[tiny-samba] ERROR ...` line and exits non-zero. Validate without
running:

```sh
make build           # makes ./tiny-samba binary
./tiny-samba check -config examples/config.yaml -passwords examples/passwords.yaml
```

(or, since `tiny-samba check` runs anywhere with the right files, you can
also `docker run --rm -v $PWD/examples:/etc/tiny-samba:ro <image> tiny-samba check`.)

## Where to put images

For your personal zot at home:
```sh
make image push REGISTRY=registry.example.com/acdtrx TAG=latest BUILDER_CONTEXT=remote-builder
```

For a public release to GHCR:
```sh
make image push REGISTRY=ghcr.io/acdtrx TAG=v0.1.0 BUILDER_CONTEXT=remote-builder
```

For Docker Hub:
```sh
make image push REGISTRY=docker.io/acdtrx TAG=latest BUILDER_CONTEXT=remote-builder
```
