import { NextRequest, NextResponse } from "next/server";
import Razorpay from "razorpay";
import { isAuthed } from "@/lib/auth";
import { insertOrder } from "@/lib/db";

export async function POST(req: NextRequest) {
  if (!(await isAuthed())) return NextResponse.json({ error: "unauthorized" }, { status: 401 });

  const body = await req.json().catch(() => ({}));
  const rupees = Number(body.amount);
  if (!Number.isFinite(rupees) || rupees < 1 || rupees > 500000) {
    return NextResponse.json({ error: "amount must be ₹1 – ₹5,00,000" }, { status: 400 });
  }
  const note = typeof body.note === "string" ? body.note.slice(0, 200) : null;
  const amountPaise = Math.round(rupees * 100);

  const rzp = new Razorpay({
    key_id: process.env.RAZORPAY_KEY_ID!,
    key_secret: process.env.RAZORPAY_KEY_SECRET!,
  });
  const order = await rzp.orders.create({
    amount: amountPaise,
    currency: "INR",
    notes: note ? { note } : undefined,
  });
  await insertOrder(order.id, amountPaise, note);

  return NextResponse.json({
    orderId: order.id,
    amount: amountPaise,
    currency: "INR",
    keyId: process.env.RAZORPAY_KEY_ID,
  });
}
