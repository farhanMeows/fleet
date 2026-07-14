import { NextResponse } from "next/server";
import { isAuthed } from "@/lib/auth";
import { listPayments } from "@/lib/db";

export async function GET() {
  if (!(await isAuthed())) return NextResponse.json({ error: "unauthorized" }, { status: 401 });
  return NextResponse.json({ payments: await listPayments() });
}
