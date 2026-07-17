import { readFile } from "fs/promises";
import path from "path";
import { NextRequest, NextResponse } from "next/server";
import { verifyToken } from "@/lib/token";

// Token-gated release downloads. Binaries live in admin/releases/ (staged by
// scripts/release.sh) and are bundled into this function via
// outputFileTracingIncludes — they are never exposed as public static files.

const ALLOWED = new Set([
  "fleet-darwin-arm64.tar.gz",
  "fleet-darwin-x86_64.tar.gz",
  "checksums.txt",
  "latest.txt",
]);

export async function GET(req: NextRequest) {
  const file = req.nextUrl.searchParams.get("f") ?? "";
  if (!ALLOWED.has(file)) {
    return NextResponse.json({ error: "unknown file" }, { status: 404 });
  }
  if (!verifyToken(req.nextUrl.searchParams.get("t"))) {
    return NextResponse.json(
      { error: "sign in at https://www.fleetdeck.in to download fleet" },
      { status: 403 },
    );
  }

  let data: Buffer;
  try {
    data = await readFile(path.join(process.cwd(), "releases", file));
  } catch {
    return NextResponse.json({ error: "release artifact missing" }, { status: 503 });
  }

  return new NextResponse(new Uint8Array(data), {
    headers: {
      "Content-Type": file.endsWith(".tar.gz") ? "application/gzip" : "text/plain; charset=utf-8",
      "Content-Disposition": `attachment; filename="${file}"`,
      "Content-Length": String(data.length),
      "Cache-Control": "no-store",
    },
  });
}
