#!/bin/sh
# fleet installer — macOS only (Apple silicon & Intel).
#
#   curl -fsSL https://fleetdeck.in/install.sh | sh
#
# BASE_URL is the only place the distribution host is named. Point it at
# wherever website/ is deployed — scripts/release.sh stages the binaries
# under /releases/ there. Swapping the host later (e.g. for a licensed
# download endpoint) means changing this one value.
set -eu

BASE_URL="${FLEET_BASE_URL:-https://fleetdeck.in}"

[ "$(uname -s)" = "Darwin" ] || {
  echo "fleet currently ships for macOS only" >&2
  exit 1
}

arch="$(uname -m)" # arm64 (Apple silicon) or x86_64 (Intel)
case "$arch" in
  arm64|x86_64) ;;
  *) echo "unsupported architecture: $arch" >&2; exit 1 ;;
esac

url="${FLEET_INSTALL_URL:-$BASE_URL/releases/fleet-darwin-$arch.tar.gz}"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "==> downloading fleet ($arch)"
curl -fsSL "$url" -o "$tmp/fleet.tar.gz"
tar -xzf "$tmp/fleet.tar.gz" -C "$tmp"
chmod +x "$tmp/fleet"

dest="${FLEET_BIN_DIR:-/usr/local/bin}"
if [ -d "$dest" ] && [ -w "$dest" ]; then
  mv "$tmp/fleet" "$dest/fleet"
elif command -v sudo >/dev/null 2>&1 && [ -z "${FLEET_BIN_DIR:-}" ]; then
  echo "==> installing to $dest (needs sudo)"
  sudo mkdir -p "$dest"
  sudo mv "$tmp/fleet" "$dest/fleet"
else
  dest="${FLEET_BIN_DIR:-$HOME/.local/bin}"
  mkdir -p "$dest"
  mv "$tmp/fleet" "$dest/fleet"
fi

echo "==> installed $("$dest/fleet" --version) to $dest/fleet"
case ":$PATH:" in
  *":$dest:"*) ;;
  *) echo "    note: $dest is not on your PATH" ;;
esac

cat <<'EOF'

next steps:
  fleet install               # wire hooks into ~/.claude/settings.json
  fleet add ~/code/my-project # register projects
  fleet up                    # launch mission control
EOF
