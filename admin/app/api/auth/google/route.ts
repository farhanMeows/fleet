import { NextRequest, NextResponse } from "next/server";
import { upsertUser } from "@/lib/db";
import { mintToken } from "@/lib/token";

// Records website sign-ups (Sign in with Google on www.fleetdeck.in).
// The Google ID token is verified server-side against Google's tokeninfo
// endpoint and our client ID before anything is stored.

const SITE_ORIGIN = "https://www.fleetdeck.in";

function cors(res: NextResponse): NextResponse {
  res.headers.set("Access-Control-Allow-Origin", SITE_ORIGIN);
  res.headers.set("Access-Control-Allow-Methods", "POST, OPTIONS");
  res.headers.set("Access-Control-Allow-Headers", "Content-Type");
  return res;
}

export async function OPTIONS() {
  return cors(new NextResponse(null, { status: 204 }));
}

export async function POST(req: NextRequest) {
  const { credential } = await req.json().catch(() => ({}));
  if (!credential) return cors(NextResponse.json({ error: "missing credential" }, { status: 400 }));

  const info = await fetch(
    `https://oauth2.googleapis.com/tokeninfo?id_token=${encodeURIComponent(credential)}`,
  ).then((r) => (r.ok ? r.json() : null));

  if (
    !info?.email ||
    info.aud !== process.env.GOOGLE_CLIENT_ID ||
    !["accounts.google.com", "https://accounts.google.com"].includes(info.iss)
  ) {
    return cors(NextResponse.json({ error: "invalid token" }, { status: 401 }));
  }

  await upsertUser({ sub: info.sub, email: info.email, name: info.name ?? null });

  // Download token: lets the site show the personal install one-liner and
  // authorizes /api/install.sh + /api/download. Sign-in still succeeds if the
  // secret is missing — the account is recorded, downloads stay locked.
  let token: string | null = null;
  let exp: number | null = null;
  try {
    ({ token, exp } = mintToken(info.email));
  } catch {
    token = null;
  }

  return cors(
    NextResponse.json({ ok: true, email: info.email, name: info.name ?? null, token, exp }),
  );
}
