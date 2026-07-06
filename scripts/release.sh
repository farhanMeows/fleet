#!/bin/sh
# Build macOS release artifacts (darwin/amd64 + darwin/arm64) and stage them
# into website/ for self-hosted distribution — no GitHub involved. Deploy the
# website/ directory to your host and the one-line installer serves from it:
#
#   scripts/release.sh v0.1.0
#   → website/install.sh                         (copied from scripts/)
#   → website/releases/fleet-darwin-arm64.tar.gz
#   → website/releases/fleet-darwin-x86_64.tar.gz
#   → website/releases/checksums.txt, latest.txt
set -eu

VERSION="${1:?usage: scripts/release.sh vX.Y.Z}"
case "$VERSION" in v*) ;; *) echo "version must start with v (got $VERSION)" >&2; exit 1 ;; esac

cd "$(dirname "$0")/.."

echo "==> building embedded web dashboard"
(cd web && npm run build)

echo "==> building darwin binaries"
OUT=website/releases
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
cp scripts/install.sh website/install.sh
echo "    $OUT/checksums.txt · $OUT/latest.txt · website/install.sh"

cat <<'EOF'

Staged. To ship: deploy the website/ directory to your host (any static
server). The install one-liner then works from your domain:

  curl -fsSL https://<your-domain>/install.sh | sh
EOF
