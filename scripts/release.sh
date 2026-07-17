#!/bin/sh
# Build macOS release artifacts (darwin/amd64 + darwin/arm64) and stage them
# into admin/releases/ — served only through the token-gated download API
# (admin.fleetdeck.in/api/download), never as public static files:
#
#   scripts/release.sh v0.1.0
#   → admin/releases/fleet-darwin-arm64.tar.gz
#   → admin/releases/fleet-darwin-x86_64.tar.gz
#   → admin/releases/checksums.txt, latest.txt
set -eu

VERSION="${1:?usage: scripts/release.sh vX.Y.Z}"
case "$VERSION" in v*) ;; *) echo "version must start with v (got $VERSION)" >&2; exit 1 ;; esac

cd "$(dirname "$0")/.."

echo "==> building embedded web dashboard"
(cd web && npm run build)

echo "==> building darwin binaries"
OUT=admin/releases
rm -rf "$OUT" dist && mkdir -p "$OUT" dist
for goarch in amd64 arm64; do
  case "$goarch" in
    amd64) label=x86_64 ;; # matches `uname -m` on Intel Macs
    arm64) label=arm64 ;;
  esac
  CGO_ENABLED=0 GOOS=darwin GOARCH=$goarch \
    go build -trimpath -ldflags "-s -w -X main.version=$VERSION" \
    -o dist/fleet ./cmd/fleet
  tar -C dist -czf "$OUT/fleet-darwin-$label.tar.gz" fleet
  rm dist/fleet
  echo "    $OUT/fleet-darwin-$label.tar.gz"
done
(cd "$OUT" && shasum -a 256 *.tar.gz > checksums.txt)
printf '%s\n' "$VERSION" > "$OUT/latest.txt"
echo "    $OUT/checksums.txt · $OUT/latest.txt"

cat <<'EOF'

Staged. To ship: deploy admin/ (the binaries ride along inside it) and the
website/ static directory. Signed-in users get their personal one-liner:

  curl -fsSL "https://admin.fleetdeck.in/api/install.sh?t=<token>" | sh
EOF
