import { createHmac, timingSafeEqual } from "crypto";
import { NextRequest, NextResponse } from "next/server";
import Razorpay from "razorpay";
import { isAuthed } from "@/lib/auth";
import { markPayment } from "@/lib/db";

// Browser-side confirmation: verifies Razorpay's checkout signature
// (HMAC of order_id|payment_id with the key secret), then double-checks the
// payment's real status via the Razorpay API. The webhook remains the
// authoritative S2S confirmation; this gives instant UI feedback.
export async function POST(req: NextRequest) {
  if (!(await isAuthed())) return NextResponse.json({ error: "unauthorized" }, { status: 401 });

  const { orderId, paymentId, signature } = await req.json().catch(() => ({}));
  if (!orderId || !paymentId || !signature) {
    return NextResponse.json({ error: "missing fields" }, { status: 400 });
  }

  const expected = createHmac("sha256", process.env.RAZORPAY_KEY_SECRET!)
    .update(`${orderId}|${paymentId}`)
    .digest("hex");
  const a = Buffer.from(expected);
  const b = Buffer.from(String(signature));
  if (a.length !== b.length || !timingSafeEqual(a, b)) {
    await markPayment({ orderId, paymentId, status: "failed" });
    return NextResponse.json({ error: "signature mismatch" }, { status: 400 });
  }

  const rzp = new Razorpay({
    key_id: process.env.RAZORPAY_KEY_ID!,
    key_secret: process.env.RAZORPAY_KEY_SECRET!,
  });
  const payment = await rzp.payments.fetch(paymentId);
  const status = payment.status === "captured" ? "captured" : "paid";
  await markPayment({
    orderId,
    paymentId,
    status,
    method: payment.method,
    amountPaise: Number(payment.amount),
  });

  return NextResponse.json({ ok: true, status });
}
