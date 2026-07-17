import { NextRequest, NextResponse } from "next/server";
import { verifyToken } from "@/lib/token";

// Token-gated installer. The website shows signed-in users:
//
//   curl -fsSL "https://admin.fleetdeck.in/api/install.sh?t=<token>" | sh
//
// The script re-embeds the same token in the binary download URL, so the
// tarball fetch below is authorized by /api/download with no extra state.

function shell(body: string, status = 200): NextResponse {
  return new NextResponse(body, {
    status,
    headers: { "Content-Type": "text/x-shellscript; charset=utf-8", "Cache-Control": "no-store" },
  });
}

export async function GET(req: NextRequest) {
  const token = req.nextUrl.searchParams.get("t");
  const user = verifyToken(token);
  if (!user) {
    return shell(
      `#!/bin/sh
echo "fleetdeck: this install link is invalid or has expired." >&2
echo "Sign in at https://www.fleetdeck.in to get a fresh one." >&2
exit 1
`,
      403,
    );
  }

  const base = req.nextUrl.origin;
  return shell(`#!/bin/sh
# fleetdeck installer — macOS only (Apple silicon & Intel).
# Issued to ${user.email}; the download link inside expires with your sign-in.
set -eu

[ "$(uname -s)" = "Darwin" ] || {
  echo "fleet currently ships for macOS only" >&2
  exit 1
}

arch="$(uname -m)" # arm64 (Apple silicon) or x86_64 (Intel)
case "$arch" in
  arm64|x86_64) ;;
  *) echo "unsupported architecture: $arch" >&2; exit 1 ;;
esac

url="${base}/api/download?f=fleet-darwin-$arch.tar.gz&t=${token}"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "==> downloading fleet ($arch)"
curl -fsSL "$url" -o "$tmp/fleet.tar.gz"
tar -xzf "$tmp/fleet.tar.gz" -C "$tmp"
chmod +x "$tmp/fleet"

dest="\${FLEET_BIN_DIR:-/usr/local/bin}"
if [ -d "$dest" ] && [ -w "$dest" ]; then
  mv "$tmp/fleet" "$dest/fleet"
elif command -v sudo >/dev/null 2>&1 && [ -z "\${FLEET_BIN_DIR:-}" ]; then
  echo "==> installing to $dest (needs sudo)"
  sudo mkdir -p "$dest"
  sudo mv "$tmp/fleet" "$dest/fleet"
else
  dest="\${FLEET_BIN_DIR:-$HOME/.local/bin}"
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
`);
}
