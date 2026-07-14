import { createHmac, timingSafeEqual } from "crypto";
import { cookies } from "next/headers";

const COOKIE = "fleetdeck_admin";

function secret(): string {
  const pw = process.env.ADMIN_PASSWORD;
  if (!pw) throw new Error("ADMIN_PASSWORD env var is not set");
  // Session-signing key derived from the password: one less env var, and
  // changing the password invalidates every session.
  return createHmac("sha256", "fleetdeck-admin-session-v1").update(pw).digest("hex");
}

function sign(value: string): string {
  return createHmac("sha256", secret()).update(value).digest("hex");
}

export function checkPassword(candidate: string): boolean {
  const expected = Buffer.from(process.env.ADMIN_PASSWORD ?? "");
  const got = Buffer.from(candidate);
  if (expected.length === 0 || expected.length !== got.length) return false;
  return timingSafeEqual(expected, got);
}

export function sessionCookieValue(): string {
  return sign("admin-session");
}

export async function setSession(): Promise<void> {
  (await cookies()).set(COOKIE, sessionCookieValue(), {
    httpOnly: true,
    secure: process.env.NODE_ENV === "production",
    sameSite: "lax",
    path: "/",
    maxAge: 7 * 24 * 3600,
  });
}

export async function clearSession(): Promise<void> {
  (await cookies()).delete(COOKIE);
}

export async function isAuthed(): Promise<boolean> {
  const c = (await cookies()).get(COOKIE)?.value ?? "";
  const expected = sessionCookieValue();
  if (c.length !== expected.length) return false;
  return timingSafeEqual(Buffer.from(c), Buffer.from(expected));
}
