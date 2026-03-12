#!/usr/bin/env sh
set -eu

REPO="husky-scheduler/husky"
BINDIR="${BINDIR:-/usr/local/bin}"
VERSION="${VERSION:-latest}"
TMPDIR="$(mktemp -d)"
ARCHIVE=""
TAG=""
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"

cleanup() {
  rm -rf "$TMPDIR"
}
trap cleanup EXIT INT TERM

usage() {
  cat <<'EOF'
Install Husky from a GitHub release.

Usage: install.sh [-b bindir] [-v version]

Options:
  -b DIR     install directory (default: /usr/local/bin or $BINDIR)
  -v VERSION release version without leading v, or 'latest'
EOF
}

while getopts "b:v:h" opt; do
  case "$opt" in
    b) BINDIR="$OPTARG" ;;
    v) VERSION="$OPTARG" ;;
    h)
      usage
      exit 0
      ;;
    *)
      usage >&2
      exit 1
      ;;
  esac
done

case "$OS" in
  linux|darwin) ;;
  *)
    echo "unsupported operating system: $OS" >&2
    exit 1
    ;;
esac

case "$ARCH" in
  x86_64|amd64) ARCH="amd64" ;;
  arm64|aarch64) ARCH="arm64" ;;
  *)
    echo "unsupported architecture: $ARCH" >&2
    exit 1
    ;;
esac

if [ "$OS" = "linux" ] && [ "$ARCH" = "arm64" ]; then
  ARCHIVE="husky_linux_arm64.tar.gz"
elif [ "$OS" = "linux" ] && [ "$ARCH" = "amd64" ]; then
  ARCHIVE="husky_linux_amd64.tar.gz"
elif [ "$OS" = "darwin" ] && [ "$ARCH" = "arm64" ]; then
  ARCHIVE="husky_darwin_arm64.tar.gz"
else
  ARCHIVE="husky_darwin_amd64.tar.gz"
fi

if [ "$VERSION" = "latest" ]; then
  TAG="$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" | awk -F '"' '/"tag_name":/ {print $4; exit}')"
else
  TAG="v$VERSION"
fi

if [ -z "$TAG" ]; then
  echo "failed to resolve release version" >&2
  exit 1
fi

BASE_URL="https://github.com/$REPO/releases/download/$TAG"
ARCHIVE_URL="$BASE_URL/$ARCHIVE"
CHECKSUM_URL="$BASE_URL/checksums.txt"

command -v curl >/dev/null 2>&1 || {
  echo "curl is required" >&2
  exit 1
}
command -v tar >/dev/null 2>&1 || {
  echo "tar is required" >&2
  exit 1
}

printf 'Downloading %s...\n' "$ARCHIVE_URL"
curl -fsSL "$ARCHIVE_URL" -o "$TMPDIR/$ARCHIVE"
curl -fsSL "$CHECKSUM_URL" -o "$TMPDIR/checksums.txt"

EXPECTED="$(grep "$ARCHIVE$" "$TMPDIR/checksums.txt" | awk '{print $1}')"
if [ -z "$EXPECTED" ]; then
  echo "checksum for $ARCHIVE not found" >&2
  exit 1
fi

if command -v sha256sum >/dev/null 2>&1; then
  ACTUAL="$(sha256sum "$TMPDIR/$ARCHIVE" | awk '{print $1}')"
elif command -v shasum >/dev/null 2>&1; then
  ACTUAL="$(shasum -a 256 "$TMPDIR/$ARCHIVE" | awk '{print $1}')"
else
  echo "sha256sum or shasum is required for checksum verification" >&2
  exit 1
fi

if [ "$EXPECTED" != "$ACTUAL" ]; then
  echo "checksum verification failed" >&2
  exit 1
fi

mkdir -p "$TMPDIR/extract"
tar -xzf "$TMPDIR/$ARCHIVE" -C "$TMPDIR/extract"
mkdir -p "$BINDIR"
install "$TMPDIR/extract/husky" "$BINDIR/husky"

printf 'Installed husky to %s\n' "$BINDIR"
