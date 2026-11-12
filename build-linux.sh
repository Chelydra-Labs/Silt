#!/usr/bin/env bash
# Linux build script for Silt. Thin wrapper that delegates packaging to the v3
# Taskfile (`wails3 task linux:package`) — the single packaging source of truth
# (build/linux/Taskfile.yml + build/linux/nfpm/nfpm.yaml). Mirrors build.sh's
# shape: version handling, frontend deps, icon gen, then the Taskfile does the
# build + .deb + .AppImage. Produces two artifacts:
#   1) AppImage — single self-contained executable (no install required).
#   2) .deb     — Debian/Ubuntu package (apt install, desktop menu, icon, and a
#                 libwebkitgtk-6.0 dependency declared via nfpm).
#
# Both artifacts rely on the host providing libwebkitgtk-6.0 (the GTK4 +
# WebKitGTK 6.0 stack that wails3 v3 builds against by default). Bundling webkit
# would bloat the packages and is intentionally avoided.
#
# Usage:  ./build-linux.sh            # prompt: bump patch version? (y/N)
#         ./build-linux.sh --no-bump  # never bump (used by CI, or quick rebuilds)
#         ./build-linux.sh --bump     # bump without prompting
set -euo pipefail

ROOT="$(cd "$(dirname "$0")" && pwd)"
cd "$ROOT"

VERSION_FILE="$ROOT/VERSION"
DIST_DIR="$ROOT/distributions"
APP_NAME="silt"

# --- helpers ---
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log_info()  { printf "${GREEN}[INFO]${NC}  %s\n" "$*"; }
log_warn()  { printf "${YELLOW}[WARN]${NC}  %s\n" "$*"; }
log_error() { printf "${RED}[ERROR]${NC} %s\n" "$*"; }

check_tool() {
    if ! command -v "$1" &> /dev/null; then
        log_error "$1 is required but not found. Install it and re-run."
        exit 1
    fi
}

# Bump the patch component of a semver string (echoes MAJOR.MINOR.PATCH+1).
bump_patch() {
    local major minor patch
    IFS='.' read -r major minor patch <<< "$1"
    patch=$((patch + 1))
    echo "${major}.${minor}.${patch}"
}

# --- args ---
# BUMP_REQUESTED: "" = prompt interactively (default), "yes" = --bump,
#                 "no" = --no-bump. CI passes --no-bump so it never blocks.
BUMP_REQUESTED=""
for arg in "$@"; do
    case "$arg" in
        --no-bump) BUMP_REQUESTED="no" ;;
        --bump)    BUMP_REQUESTED="yes" ;;
        -h|--help)
            echo "Usage: $0 [--no-bump|--bump]"
            echo "  (default)  prompt whether to bump the patch version (y/N)."
            echo "  --no-bump  never bump (CI / quick local rebuilds)."
            echo "  --bump     bump without prompting."
            echo "Releases are tagged automatically by the Release workflow on merge to main."
            exit 0 ;;
        *) log_error "Unknown option: $arg"; exit 1 ;;
    esac
done

# --- prereq checks ---
check_tool go
check_tool node
check_tool npm
check_tool wails3
check_tool gcc

# webkitgtk-6.0 (GTK4) is required: wails3 v3's default Linux stack pkg-configs
# gtk4 + webkitgtk-6.0. Ubuntu 24.04+ / Debian 13 / Fedora 39+ ship it natively.
if ! pkg-config --exists webkitgtk-6.0 2>/dev/null; then
    log_error "webkitgtk-6.0 not found. Install the dev package, e.g.:"
    log_error "  Ubuntu 24.04: sudo apt install libwebkitgtk-6.0-dev libgtk-4-dev"
    exit 1
fi

# --- read version & decide whether to advance -------------------------------
# CI releases on merge; locally we ask before advancing so test builds don't
# create spurious versions. --bump/--no-bump skip the prompt (CI uses --no-bump).
if [ ! -f "$VERSION_FILE" ]; then
    echo "0.1.0" > "$VERSION_FILE"
    log_info "Created VERSION file with 0.1.0"
fi
OLD_VERSION=$(tr -d '[:space:]' < "$VERSION_FILE")

CANDIDATE_VERSION="$(bump_patch "$OLD_VERSION")"

if [[ "$BUMP_REQUESTED" == "yes" ]]; then
    BUMP="yes"
elif [[ "$BUMP_REQUESTED" == "no" ]]; then
    BUMP="no"
else
    # Prompt only on an interactive TTY. In any non-interactive context
    # (piped input, CI without --no-bump) default to NO bump so we never block.
    if [[ -t 0 ]]; then
        read -rp "Bump patch version ${OLD_VERSION} -> ${CANDIDATE_VERSION}? [y/N] " ans || ans=""
        case "${ans:-n}" in
            y|Y|yes|YES) BUMP="yes" ;;
            *)           BUMP="no" ;;
        esac
    else
        BUMP="no"
    fi
fi

if [[ "$BUMP" == "yes" ]]; then
    VERSION="$CANDIDATE_VERSION"
    log_info "Building version: $OLD_VERSION -> $VERSION"
else
    VERSION="$OLD_VERSION"
    log_info "Building version: $VERSION (no bump)"
fi

# --- frontend + icon ---
log_info "Installing frontend dependencies..."
(cd "$ROOT/frontend" && npm install)

log_info "Generating app icon from logo.svg..."
NODE_PATH="$ROOT/frontend/node_modules" node "$ROOT/scripts/generate-icon.mjs" \
    "$ROOT/frontend/src/assets/logo.svg" \
    "$ROOT/build/appicon.png"

# Clean previous build artifacts (after frontend deps so dist/ can be rebuilt
# by the Taskfile's build:frontend dep).
rm -rf "$ROOT/build/bin"

# The v3 linux Taskfile is the single packaging source of truth: it builds
# (linux:build:native against the default GTK4 + WebKitGTK 6.0 stack), then
# creates the .AppImage + .deb. VERSION + GOARCH are exported so nfpm can
# interpolate them in the .deb. GOARCH defaults to the host arch so the .deb's
# arch metadata matches the binary the native build actually produced (the
# Taskfile's build:native targets the host ARCH). Override with GOARCH=... for
# cross-arch packaging.
log_info "Building + packaging (.AppImage + .deb) with Wails v3 linux Taskfile..."
case "$(uname -m)" in
  aarch64|arm64) DEFAULT_GOARCH=arm64 ;;
  *) DEFAULT_GOARCH=amd64 ;;
esac
export VERSION
export GOARCH="${GOARCH:-$DEFAULT_GOARCH}"
wails3 task linux:package

# --- collect artifacts into the distribution directory ---
BUILD_DIR="$DIST_DIR/v${VERSION}"
rm -rf "$BUILD_DIR"
mkdir -p "$BUILD_DIR"

shopt -s nullglob
found=0
for f in "$ROOT/build/bin"/*.deb "$ROOT/build/bin"/*.AppImage; do
    cp "$f" "$BUILD_DIR/"
    log_info "  -> $BUILD_DIR/$(basename "$f")"
    found=$((found + 1))
done
shopt -u nullglob

if [ "$found" -eq 0 ]; then
    log_error "No .deb or .AppImage artifacts found in build/bin/. Packaging may have failed."
    exit 1
fi

# --- persist new version (only on success, and only if we bumped) ---
if [[ "$BUMP" == "yes" ]]; then
    echo "$VERSION" > "$VERSION_FILE"
    log_info "Version bumped to $VERSION"
fi

# --- summary ---
echo ""
echo "  ┌─────────────────────────────────────────────┐"
echo "  │  Build complete — version $VERSION"
echo "  │  Artifacts: $found (.deb + .AppImage)"
echo "  │  Location : $BUILD_DIR"
echo "  └─────────────────────────────────────────────┘"
