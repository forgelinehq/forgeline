#!/bin/sh
# Regenerates internal/server/ui/lib/icons.js from the pinned Lucide release (ISC license). The tarball is checked
# against the sha256 below before anything in it is read. Add icon names in scripts/icons/main.go.
set -eu

VERSION=1.70.0
SUM=3d1f09df1c468abe512ee6d6596db0dc773fa46c61050e365f0c5fee5120f16a

ROOT=$(cd "$(dirname "$0")/.." && pwd)
mkdir -p "$ROOT/.tools"
DIR=$(mktemp -d "$ROOT/.tools/lucide.XXXXXX")
trap 'rm -rf "$DIR"' EXIT
curl -fsSL --retry 4 --retry-delay 5 -o "$DIR/lucide.tgz" "https://registry.npmjs.org/lucide-static/-/lucide-static-$VERSION.tgz"
GOT=$(sha256sum "$DIR/lucide.tgz" 2>/dev/null | awk '{print $1}' || shasum -a 256 "$DIR/lucide.tgz" | awk '{print $1}')
if [ "$GOT" != "$SUM" ]; then
  echo "scripts/icons.sh: checksum mismatch for lucide-static $VERSION; refusing to use it" >&2
  exit 1
fi
tar -xzf "$DIR/lucide.tgz" -C "$DIR" package/icons package/LICENSE
cd "$ROOT"
go run ./scripts/icons "$DIR/package/icons" "$DIR/package/LICENSE" "$VERSION" > "$DIR/icons.js"
mv "$DIR/icons.js" internal/server/ui/lib/icons.js
echo "internal/server/ui/lib/icons.js: Lucide $VERSION"
