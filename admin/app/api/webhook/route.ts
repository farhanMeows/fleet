import { createHmac, timingSafeEqual } from "crypto";
import { NextRequest, NextResponse } from "next/server";
import { markInvoicePaid, markPayment } from "@/lib/db";

// Server-to-server confirmation from Razorpay. This is the source of truth:
// it fires even if the buyer closed the browser mid-flow. Configure in the
// Razorpay dashboard: Webhooks → https://admin.fleetdeck.in/api/webhook with
// events payment.captured + payment.failed, using RAZORPAY_WEBHOOK_SECRET.
export async function POST(req: NextRequest) {
  const raw = await req.text();
  const signature = req.headers.get("x-razorpay-signature") ?? "";
  const expected = createHmac("sha256", process.env.RAZORPAY_WEBHOOK_SECRET!)
    .update(raw)
    .digest("hex");
  const a = Buffer.from(expected);
  const b = Buffer.from(signature);
  if (a.length !== b.length || !timingSafeEqual(a, b)) {
    return NextResponse.json({ error: "bad signature" }, { status: 400 });
  }

  const event = JSON.parse(raw);

  // Invoice payment links resolve via payment_link.paid.
  if (event.event === "payment_link.paid") {
    const link = event?.payload?.payment_link?.entity;
    const pay = event?.payload?.payment?.entity;
    if (link?.id) await markInvoicePaid(link.id, pay?.id ?? "");
    return NextResponse.json({ ok: true });
  }

  const entity = event?.payload?.payment?.entity;
  if (!entity?.order_id) return NextResponse.json({ ok: true, skipped: true });

  if (event.event === "payment.captured") {
    await markPayment({
      orderId: entity.order_id,
      paymentId: entity.id,
      status: "captured",
      method: entity.method,
      amountPaise: Number(entity.amount),
    });
  } else if (event.event === "payment.failed") {
    await markPayment({
      orderId: entity.order_id,
      paymentId: entity.id,
      status: "failed",
      method: entity.method,
      amountPaise: Number(entity.amount),
    });
  }
  return NextResponse.json({ ok: true });
}
