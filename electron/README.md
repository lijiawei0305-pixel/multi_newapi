# New API Electron Desktop App

This directory contains the Electron wrapper for New API. The packaged desktop app includes the Go backend and serves the same Default and Classic frontend themes as the server build.

## Prerequisites

- Go (the version required by the root `go.mod`)
- Bun for the frontend workspace
- Node.js and npm for the Electron package (`electron/package-lock.json` is authoritative)
- The native packaging tools required by electron-builder for the target platform

## Development

Development mode expects the Go backend and a frontend development server to be started separately. A clean checkout first needs the directories referenced by Go's embedded assets and the shared frontend dependencies:

```bash
for dir in web/default/dist web/classic/dist; do
  mkdir -p "$dir"
  test -f "$dir/index.html" || printf '<!doctype html>\n' > "$dir/index.html"
done
cd web
bun install --frozen-lockfile --linker=isolated
```

Then, from the repository root, use three terminals:

```bash
# Terminal 1: backend on port 3000
go run .

# Terminal 2: Default frontend on port 5173
cd web/default
bun run dev

# Terminal 3: Electron wrapper
cd electron
npm ci
npm run dev-app
```

`npm run dev-app` is the development Electron script. The Classic theme is included in production builds; for standalone Classic frontend development, run `bun run dev` from `web/classic` instead.

## Production build

Run the repository build driver from the repository root:

```bash
./electron/build.sh
```

`build.sh` is the supported end-to-end path. It:

1. installs the frontend workspace with Bun;
2. builds both `web/default` and `web/classic` with the same `VITE_REACT_APP_VERSION`;
3. builds the Go backend with that version embedded through linker flags;
4. runs the backend with `--version` and aborts if the result differs from the requested version;
5. installs Electron dependencies with `npm ci` and packages the current platform.

By default, the version is derived from `git describe --tags --always --dirty`. To request an explicit release version, pass it to the same driver:

```bash
VERSION=v1.2.3 ./electron/build.sh
```

Do not bypass the driver with `npm run build:*` for a release: those scripts only run electron-builder and do not build or version-check the Default frontend, Classic frontend, or Go backend.

Packaged artifacts are written to `electron/dist/`:

- macOS: `.dmg` and `.zip`
- Windows: NSIS installer and portable executable
- Linux: `.AppImage` and `.deb`

## Runtime configuration

The wrapper uses port 3000 for the embedded backend. Runtime data is stored below Electron's per-user application data directory:

- macOS: `~/Library/Application Support/New API/`
- Windows: `%APPDATA%/New API/`
- Linux: `~/.config/New API/`
