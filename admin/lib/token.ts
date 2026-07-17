import { createHmac, timingSafeEqual } from "crypto";

// Download tokens: HMAC-signed `payload.sig` strings handed out after Google
// sign-in and required by /api/install.sh and /api/download. Stateless on
// purpose — no session table, revocation happens by rotating the secret.

const TOKEN_TTL_MS = 7 * 24 * 60 * 60 * 1000;

function secret(): string {
  const s = process.env.DOWNLOAD_SECRET;
  if (!s) throw new Error("DOWNLOAD_SECRET is not set");
  return s;
}

function sign(payload: string): string {
  return createHmac("sha256", secret()).update(payload).digest("base64url");
}

export function mintToken(email: string): { token: string; exp: number } {
  const exp = Date.now() + TOKEN_TTL_MS;
  const payload = Buffer.from(JSON.stringify({ e: email, x: exp })).toString("base64url");
  return { token: `${payload}.${sign(payload)}`, exp };
}

export function verifyToken(token: string | null): { email: string } | null {
  if (!token) return null;
  const [payload, sig] = token.split(".");
  if (!payload || !sig) return null;
  try {
    const want = Buffer.from(sign(payload));
    const got = Buffer.from(sig);
    if (want.length !== got.length || !timingSafeEqual(want, got)) return null;
    const { e, x } = JSON.parse(Buffer.from(payload, "base64url").toString());
    if (typeof e !== "string" || typeof x !== "number" || Date.now() > x) return null;
    return { email: e };
  } catch {
    return null;
  }
}
