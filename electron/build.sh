#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR=$(cd "$(dirname "$0")/.." && pwd)
VERSION=${VERSION:-$(git -C "$ROOT_DIR" describe --tags --always --dirty)}
LDFLAGS="-s -w -X github.com/QuantumNous/new-api/common.Version=$VERSION"

verify_backend_version() {
    local binary=$1
    local actual
    actual=$("$binary" --version | tr -d '\r')
    if [ "$actual" != "$VERSION" ]; then
        echo "Backend version '$actual' does not match '$VERSION'" >&2
        exit 1
    fi
}

echo "Building New API Electron App ($VERSION)..."

echo "Step 1: Building frontends..."
cd "$ROOT_DIR/web"
bun install --frozen-lockfile --linker=isolated
(cd default && VITE_REACT_APP_VERSION="$VERSION" bun run build)
(cd classic && VITE_REACT_APP_VERSION="$VERSION" bun run build)

echo "Step 2: Building Go backend..."
cd "$ROOT_DIR"

if [[ "$OSTYPE" == "darwin"* ]]; then
    echo "Building for macOS..."
    CGO_ENABLED=1 go build -ldflags="$LDFLAGS" -o new-api
    verify_backend_version ./new-api
    cd electron
    npm ci
    npm run build:mac
elif [[ "$OSTYPE" == "linux-gnu"* ]]; then
    echo "Building for Linux..."
    CGO_ENABLED=1 go build -ldflags="$LDFLAGS" -o new-api
    verify_backend_version ./new-api
    cd electron
    npm ci
    npm run build:linux
elif [[ "$OSTYPE" == "msys" || "$OSTYPE" == "cygwin" || "$OSTYPE" == "win32" ]]; then
    echo "Building for Windows..."
    CGO_ENABLED=1 go build -ldflags="$LDFLAGS" -o new-api.exe
    verify_backend_version ./new-api.exe
    cd electron
    npm ci
    npm run build:win
else
    echo "Unknown OS, building for current platform..."
    CGO_ENABLED=1 go build -ldflags="$LDFLAGS" -o new-api
    verify_backend_version ./new-api
    cd electron
    npm ci
    npm run build
fi

echo "Build complete! Check electron/dist/ for output."
