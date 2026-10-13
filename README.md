# tiny-samba

A small, opinionated container that wraps Samba's `smbd` with a single
declarative config file and a single passwords file. Built to run on Docker
**and** on [wisp](https://github.com/acdtrx/wisp) without changes.

**What it gives you:**

- One YAML file declares shares, users, per-user RW/RO access.
- One YAML file holds passwords (plaintext or pre-computed NT hashes).
- Hot reload: change a password or add a share, run `tiny-samba reload`,
  no smbd restart, no dropped sessions.
- A single static Go binary (the wrapper) + Debian + Samba in one image.
- Sane defaults baked in (force_user, masks, SMB3 floor, `map to guest = bad
  user`, no spooler, etc.).
- Discoverable from Windows Explorer's Network sidebar (WS-Discovery + LLMNR
  via a bundled `wsdd2` sidecar) and from macOS Finder (Apple SMB extensions,
  AAPL icon model).

**What it isn't:**

- A reimplementation of SMB. Samba does that.
- An Active Directory member or DC.
- A fileshare with per-user on-disk ownership (we use `force user` for
  uniform host-side ownership; SMB-level access control is enforced by smbd).

---

## Quick start (Docker)

```sh
# 1. Build the image (or pull from a registry once one's published — see below)
git clone https://github.com/acdtrx/tiny-samba && cd tiny-samba
make image                                     # tags as localhost/tiny-samba:dev

# 2. Edit config + passwords
cp examples/config.yaml    /path/to/tiny-samba.yaml
cp examples/passwords.yaml /path/to/tiny-samba-passwords.yaml
$EDITOR /path/to/tiny-samba.yaml /path/to/tiny-samba-passwords.yaml

# 3. Run the container
docker run -d --name tiny-samba \
    --cap-add=NET_BIND_SERVICE \
    -v /path/to/tiny-samba.yaml:/etc/tiny-samba/config.yaml:ro \
    -v /path/to/tiny-samba-passwords.yaml:/etc/tiny-samba/passwords.yaml:ro \
    -v /path/to/data:/data \
    --tmpfs /var/lib/samba \
    -p 445:445 \
    -p 3702:3702/udp -p 5357:5357 \
    localhost/tiny-samba:dev

# 4. Connect from any LAN client
#   smb://<host-ip>/documents
#   \\<host-ip>\documents
```

(Substitute your registry's image reference for `localhost/tiny-samba:dev`
once you've pushed it somewhere reachable.)

Make sure `/path/to/data` (and its `documents/`, `media/`, etc.
subdirectories from your config) are owned by UID 1000 (or whatever
`server.data_uid` you set), so smbuser inside the container can write to it.

## Quick start (wisp)

1. Push the image to your wisp-reachable registry (zot, GHCR, etc.).
2. **Image Library** → import or pick the image.
3. **Create container** with that image. After create:
   - **General → runAsRoot: true** (required so smbd can `setuid` per session).
   - **Mounts**:
     - `/etc/tiny-samba/config.yaml` (file, read-only) — your config.
     - `/etc/tiny-samba/passwords.yaml` (file, read-only) — your passwords.
     - `/data` (directory, read-write, **`containerOwnerUid: 1000`**) —
       share roots underneath. The `containerOwnerUid` must match
       `server.data_uid` from your config so smbuser's writes (UID 1000
       inside the container) land on the host as your deploy user. If
       left at the default 0, files will end up `nobody`-owned (overflow
       UID 65534) and the deploy user can't clean them up without sudo.
     - `/var/lib/samba` (directory, read-write) — Samba runtime state.
       Leave `containerOwnerUid` at default 0 (smbd writes as root).
   - **Network**: bridge (default).
4. Start the container. Find its DHCP'd IP from the wisp UI.
5. Connect: `smb://<container-ip>/documents`.

## CLI

```
tiny-samba serve     # container entrypoint (you don't usually run this)
tiny-samba reload    # re-apply config and passwords without restarting smbd
tiny-samba check     # validate without applying (exit 0 if OK)
tiny-samba list      # print declared users and shares
tiny-samba version
```

`reload`, `check`, and `list` are runnable on the host (against the YAML
files) when smbd is offline, OR inside the running container via
`docker exec` / wisp console.

## Configuration

### `config.yaml`

```yaml
server:
  workgroup: WORKGROUP
  netbios_name: tiny-samba
  data_uid: 1000
  min_protocol: SMB3        # or SMB2, SMB1

users:
  - alice
  - bob

shares:
  documents:
    path: /data/documents
    comment: Shared documents
    access:
      alice: rw
      bob: ro
  public:
    path: /data/public
    guest: true             # anyone, read-only
```

### `passwords.yaml`

```yaml
alice: hunter2                                       # plaintext
bob:   $NT$8846F7EAEE8FB117AD06BDD830B7586C          # pre-computed NT hash
```

## Hot reload

After editing either file:

```sh
docker exec tiny-samba tiny-samba reload
# or, on wisp, click Reload in the container view (when the App module ships)
```

`reload` re-reads both files, regenerates Unix users and Samba's passdb,
writes a fresh `smb.conf`, and runs `smbcontrol all reload-config`. No smbd
restart. Active sessions persist; new authentications use the new
passwords/permissions.

A few changes still need a full container restart:

- `server.workgroup`
- `server.netbios_name`
- `server.data_uid`

For these, `tiny-samba reload` will warn and indicate restart-required.

## File ownership on the host

All on-disk SMB I/O happens as UID `server.data_uid` (default 1000) inside
the container. SMB-level access control (who can read/write each share) is
enforced by smbd, *not* by filesystem permissions.

- **Docker**: pick a `data_uid` that matches the host owner of `/data`
  (typically 1000).
- **Wisp**: set the `/data` mount's `containerOwnerUid` to match
  `server.data_uid` (1000). Wisp's per-mount idmap then translates
  container UID 1000 ↔ wisp deploy UID, so host-side files are cleanly
  owned by the deploy user. Without this override, smbuser's writes
  appear as `nobody:65534` on disk.

## Filesystems without extended attributes

Samba keeps Windows and macOS metadata (DOS attributes, creation time,
alternate data streams such as the "downloaded from the internet" tag,
Finder info) in extended attributes. exFAT, vfat and some CIFS mounts have
none, and Samba then fails the request instead of falling back — Explorer
copies abort with "Item not found" / robocopy `ERROR 50`.

On every start and reload, tiny-samba probes each share path with a
throwaway `user.*` xattr. Shares that can't store one get, in their own
section:

```
vfs objects = catia          # (only when Apple extensions are on)
store dos attributes = no
ea support = no
```

and a `WARN` line in the log. Clients then treat the share like a plain USB
stick: copies work, but DOS attributes and creation times aren't kept,
streams are dropped, and macOS falls back to `._` AppleDouble files.

## Connecting from Windows

### Discoverability

The container bundles `wsdd2`, a WS-Discovery + LLMNR responder. Windows
10/11 machines on the same L2 segment will find the server in **File
Explorer → Network** within ~30 seconds of startup, no manual mapping
needed. Double-click the icon to browse shares.

If you don't want this — disable it with `server.wsd: false` in
`config.yaml`. The supervisor will skip launching wsdd2; smbd is unaffected.

Caveat: WSD uses UDP multicast on `239.255.255.250:3702`. With wisp /
macvlan-style networking each container is first-class on the LAN, so this
works out of the box. With stock Docker bridge networking, multicast does
not cross the bridge — you'll need `--network host` for Windows
discoverability to actually function (the WSD ports being published is not
sufficient).

### Guest shares on Windows 11 24H2+

If you defined a `guest: true` share and `\\<ip>\<share>` fails with "an
extended error has occurred" (while authenticated shares work), Windows is
blocking the connection client-side. Run these once in **elevated
PowerShell** on the Windows machine:

```powershell
# 1. Allow guest sessions (off by default since Win10 1709)
reg add "HKLM\SYSTEM\CurrentControlSet\Services\LanmanWorkstation\Parameters" `
    /v AllowInsecureGuestAuth /t REG_DWORD /d 1 /f

# 2. Stop *requiring* SMB signing (24H2+ default; signing is incompatible
#    with guest sessions, which have no session key to derive a signing key)
Set-SmbClientConfiguration -RequireSecuritySignature $false -Confirm:$false
```

Both settings are per-machine and persistent. They weaken the SMB client's
posture against LAN MITM for *all* servers it talks to — fine for a homelab
guest share, less fine on a work laptop. For anything sensitive, prefer an
authenticated user saved in Credential Manager over guest.

## Building

```sh
# Build the image (needs Docker; doesn't need Go locally)
make image

# Push to your registry
make image push REGISTRY=registry.lan/zot

# Build on a remote Docker daemon (e.g. you're on a Mac targeting Linux/amd64).
# Requires a Docker context configured with `docker context create remote-builder ...`.
make image push BUILDER_CONTEXT=remote-builder REGISTRY=registry.lan/zot

# Run unit tests
make test

# Local smoke test (binds to host:1445)
make run-local
```

## More documentation

- [BUILD.md](BUILD.md) — full Make target reference, Docker contexts (local
  vs remote builders), troubleshooting common build errors.
- [docs/wisp-app-module.md](docs/wisp-app-module.md) — design notes for a
  future dedicated wisp App module (shares + users via wisp UI forms instead
  of YAML editing).

## Acknowledgements

The container image bundles software from upstream projects, each
distributed under its own license:

- [Samba](https://www.samba.org/) — GPL-3.0-or-later. The actual SMB
  server. tiny-samba is a thin configuration wrapper around `smbd`.
- [wsdd2](https://github.com/Netgear/wsdd2) — GPL-3.0. WS-Discovery +
  LLMNR responder for Windows Explorer discoverability.
- [tini](https://github.com/krallin/tini) — MIT. PID 1 / signal handler.
- [Debian](https://www.debian.org/) base image — various FLOSS licenses.

These are aggregated into the OCI image at build time via `apt`; their
binaries remain under their respective upstream licenses. tiny-samba's
own code (this repository) is MIT (see [LICENSE](LICENSE)).

## License

[MIT](LICENSE) © Alex NOVAC
