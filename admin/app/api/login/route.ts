import { NextRequest, NextResponse } from "next/server";
import { checkPassword, setSession } from "@/lib/auth";

export async function POST(req: NextRequest) {
  const { password } = await req.json().catch(() => ({ password: "" }));
  if (!checkPassword(String(password ?? ""))) {
    return NextResponse.json({ error: "wrong password" }, { status: 401 });
  }
  await setSession();
  return NextResponse.json({ ok: true });
}
