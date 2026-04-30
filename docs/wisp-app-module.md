# Implementing tiny-samba as a wisp App module

This is a **future-work** document. v1 of tiny-samba ships as a generic
container that runs on wisp via the standard "create container, configure
mounts, start" flow. This doc describes what implementing a dedicated
**wisp App** for tiny-samba would involve, so the work is well-scoped when
we get to it.

For background on what an App is in wisp, read
`wisp/docs/spec/CUSTOM-APPS.md` first.

## Why bother

Without an App, the user manages tiny-samba through the generic UI:

- **Mounts section**: hand-edit two YAML files in wisp's mount file editor.
- **Restart / Reload**: container restart from the lifecycle controls.
- **Secrets**: passwords are visible in the file editor (because wisp can't
  know that mount file is sensitive).

With an App, the user gets:

- A dedicated **Shares + Users** form per container — name, comment, path,
  per-user RW/RO checkboxes, no YAML.
- **Password fields** that follow wisp's secret pattern: `secret: true`,
  write-only, masked in API responses, never visible after entry.
- A **Reload** button that calls `tiny-samba reload` via
  `execInContainer` — no restart, no dropped sessions.
- Validation in the wisp UI (typos in user names, share path collisions,
  etc.) before the container ever sees a bad config.

## What needs to be built

### 1. Backend module

Create `wisp/backend/src/lib/linux/containerManager/apps/tinySamba.js`,
mirroring the structure of `apps/caddy.js`.

Required exports:

| Function | Purpose |
|---|---|
| `getDefaultAppConfig()` | Empty server block, no users, no shares. |
| `validateAppConfig(appConfig)` | Same checks tiny-samba does, ported to JS. Throws `INVALID_APP_CONFIG` on failure. |
| `generateDerivedConfig(appConfig)` | Produces the env / mounts / mountContents (see below). |
| `maskSecrets(appConfig)` | Replace each user's `password` with `{ isSet: bool }`. |
| `getReloadCommand()` | Returns `['tiny-samba', 'reload']`. |

#### `appConfig` shape

```js
{
  server: { workgroup, netbiosName, dataUid, minProtocol },
  users: [
    { name: 'alice', password: { value: 'hunter2', secret: true } },
    { name: 'bob',   password: { value: '$NT$...', secret: true } },
  ],
  shares: [
    {
      name: 'documents',
      path: '/data/documents',
      comment: 'Shared documents',
      guest: false,
      access: [
        { user: 'alice', level: 'rw' },
        { user: 'bob',   level: 'ro' },
      ],
    },
    { name: 'public', path: '/data/public', guest: true },
  ],
}
```

(Arrays-of-objects rather than maps — easier to render as form rows in the
UI.)

#### `generateDerivedConfig` output

```js
{
  env: {},  // none — passwords go via the file mount, not env vars.
  mounts: [
    { type: 'file',      name: 'config',    containerPath: '/etc/tiny-samba/config.yaml',    readonly: true },
    { type: 'file',      name: 'passwords', containerPath: '/etc/tiny-samba/passwords.yaml', readonly: true },
    { type: 'directory', name: 'data',      containerPath: '/data',                          readonly: false },
    // /var/lib/samba should be tmpfs once wisp supports tmpfs mounts.
    // Until then, declare it as a Local directory mount and accept the
    // (small) on-disk write traffic.
    { type: 'directory', name: 'samba-state', containerPath: '/var/lib/samba', readonly: false },
  ],
  mountContents: {
    config:    yamlDump(toConfigYaml(appConfig)),
    passwords: yamlDump(toPasswordsYaml(appConfig)),
  },
}
```

Conversion helpers:

```js
function toConfigYaml(appConfig) {
  return {
    server: appConfig.server,
    users:  appConfig.users.map(u => u.name),
    shares: Object.fromEntries(appConfig.shares.map(sh => [
      sh.name,
      {
        path: sh.path,
        comment: sh.comment || undefined,
        guest: sh.guest || undefined,
        access: sh.guest ? undefined : Object.fromEntries(
          sh.access.map(a => [a.user, a.level])
        ),
      },
    ])),
  };
}

function toPasswordsYaml(appConfig) {
  return Object.fromEntries(
    appConfig.users
      .filter(u => u.password?.value)
      .map(u => [u.name, u.password.value])
  );
}
```

### 2. Backend registration

Add to `wisp/backend/src/lib/linux/containerManager/apps/appRegistry.js`:

```js
import { tinySambaAppModule } from './tinySamba.js';

const APPS = {
  // ... existing apps
  'tiny-samba': {
    label: 'Tiny Samba',
    description: 'Lightweight SMB file server',
    defaultImage: 'ghcr.io/acdtrx/tiny-samba:latest',
    allowCustomImage: true,
    module: tinySambaAppModule,
  },
};
```

The container created via this App should default to `runAsRoot: true`
since smbd needs UID 0 to bind 445 and `setuid` per session. There isn't a
clean way to set this from the App module today (it's a top-level
`container.json` field, not part of `appConfig`); options:

- Have the wisp create flow special-case Apps that need root.
- Or add a `requiresRoot: true` flag to the App registry entry that the
  create-container handler reads when populating `runAsRoot`.

The latter is the cleaner change and would benefit other future Apps too.

### 3. Frontend component

Create `wisp/frontend/src/apps/TinySambaAppConfig.jsx`, registered in
`wisp/frontend/src/apps/appRegistry.js` to mirror the backend entry.

UI sections:

- **Server**: workgroup, netbios name, data UID, min protocol (dropdown).
- **Users**: table with `+ Add user`. Each row: name (text), password
  (secret field — empty placeholder when `isSet: true`, never shows the
  stored value).
- **Shares**: table with `+ Add share`. Each row: name, path, comment,
  guest checkbox. Expandable per-user access subtable when not guest;
  per-user dropdown (rw / ro) per declared user.
- **Reload** button (only enabled when container is running) that calls
  the standard wisp App reload endpoint.

Use the `secret-field` pattern from `ContainerEnvSection`: the API returns
`{ isSet: bool, value: null }` for already-set passwords; the UI shows a
"●●●●" placeholder; submitting an unchanged field omits it from the patch
so the existing value is preserved.

### 4. Documentation

- Add the App to `wisp/docs/spec/CUSTOM-APPS.md`'s App-list section.
- Cross-reference here from this doc with a "→ implemented" note.

## Idmap considerations

Once wisp supports per-mount idmapped mounts, this App should set the
following on the `data` mount:

```js
{
  type: 'directory',
  name: 'data',
  containerPath: '/data',
  readonly: false,
  uidMappings: [{ containerID: appConfig.server.dataUid, hostID: deployUid, size: 1 }],
  gidMappings: [{ containerID: appConfig.server.dataUid, hostID: deployGid, size: 1 }],
}
```

The `samba-state` mount only needs an idmap for UID 0 (smbd writes its tdb
files as root). Once that mount is tmpfs, this concern goes away — tmpfs
files are container-internal only, host ownership is moot.

## Live reload semantics — what tiny-samba supports

These changes hot-reload (no restart):

- Add / remove user (declare in YAML).
- Change password.
- Add / remove share.
- Change per-user access level.
- Change share path (smbd picks up on next session).
- Change share comment / guest flag.

These require a container restart (set `pendingRestart: true`):

- `server.workgroup`
- `server.netbios_name`
- `server.data_uid`

The wisp App should compare old vs new `appConfig` on save and set
`pendingRestart` accordingly.

## Estimated effort

Roughly aligned with the Caddy module's size (`apps/caddy.js` is ~200 LOC).

- Backend module: ~250 LOC + tests
- Frontend component: ~400 LOC (Forms are larger than backend)
- Image-with-runAsRoot wiring: small if `requiresRoot` flag exists; ~50 LOC
  diff to add the flag
- Documentation updates: ~20 lines

Total: a focused 1–2 day project once the standalone container is shaken
out and we know what bites.
