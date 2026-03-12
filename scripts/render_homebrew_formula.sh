#!/usr/bin/env sh
set -eu

VERSION="${1:-}"
CHECKSUMS_FILE="${2:-dist/checksums.txt}"
OUTPUT_FILE="${3:-packaging/homebrew/husky.rb}"
TEMPLATE_FILE="${4:-packaging/homebrew/husky.rb.tmpl}"

if [ -z "$VERSION" ]; then
  echo "usage: $0 <version> [checksums-file] [output-file] [template-file]" >&2
  exit 1
fi

if [ ! -f "$CHECKSUMS_FILE" ]; then
  echo "checksums file not found: $CHECKSUMS_FILE" >&2
  exit 1
fi

amd64_sha="$(grep 'husky_darwin_amd64.tar.gz$' "$CHECKSUMS_FILE" | awk '{print $1}')"
arm64_sha="$(grep 'husky_darwin_arm64.tar.gz$' "$CHECKSUMS_FILE" | awk '{print $1}')"

if [ -z "$amd64_sha" ] || [ -z "$arm64_sha" ]; then
  echo "darwin checksums not found in $CHECKSUMS_FILE" >&2
  exit 1
fi

sed \
  -e "s/__VERSION__/$VERSION/g" \
  -e "s/__SHA256_DARWIN_AMD64__/$amd64_sha/g" \
  -e "s/__SHA256_DARWIN_ARM64__/$arm64_sha/g" \
  "$TEMPLATE_FILE" > "$OUTPUT_FILE"

echo "wrote $OUTPUT_FILE"
