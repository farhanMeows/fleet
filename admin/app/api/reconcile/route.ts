import { NextResponse } from "next/server";
import Razorpay from "razorpay";
import { isAuthed } from "@/lib/auth";
import { listInvoices, listPayments, markInvoicePaid, markPayment } from "@/lib/db";

// Pull the truth from Razorpay for anything the webhook may have missed
// (e.g. payments made before the webhook was configured). Invoices stuck at
// "sent" and orders stuck at "created"/"paid" are re-checked via the API;
// markPayment's status ranking already prevents any downgrade.

export async function POST() {
  if (!(await isAuthed())) return NextResponse.json({ error: "unauthorized" }, { status: 401 });

  const rzp = new Razorpay({
    key_id: process.env.RAZORPAY_KEY_ID!,
    key_secret: process.env.RAZORPAY_KEY_SECRET!,
  });

  let invoicesPaid = 0;
  let paymentsUpdated = 0;
  const errors: string[] = [];

  for (const inv of await listInvoices(100)) {
    if (inv.status !== "sent" || !inv.pay_link_id) continue;
    try {
      const link = (await rzp.paymentLink.fetch(inv.pay_link_id)) as unknown as {
        status?: string;
        payments?: Array<{ payment_id?: string }> | null;
      };
      if (link.status === "paid") {
        await markInvoicePaid(inv.pay_link_id, link.payments?.[0]?.payment_id ?? "");
        invoicesPaid++;
      }
    } catch {
      errors.push(`invoice ${inv.number}`);
    }
  }

  for (const p of await listPayments(100)) {
    if (p.status === "captured" || p.status === "failed") continue;
    try {
      const res = (await rzp.orders.fetchPayments(p.order_id)) as unknown as {
        items?: Array<{ id: string; status: string; method?: string; amount: number | string }>;
      };
      const attempts = res.items ?? [];
      const captured = attempts.find((a) => a.status === "captured");
      const settled = captured ?? (attempts.length && attempts.every((a) => a.status === "failed") ? attempts[0] : null);
      if (settled) {
        await markPayment({
          orderId: p.order_id,
          paymentId: settled.id,
          status: captured ? "captured" : "failed",
          method: settled.method,
          amountPaise: Number(settled.amount),
        });
        paymentsUpdated++;
      }
    } catch {
      errors.push(`order ${p.order_id}`);
    }
  }

  return NextResponse.json({ invoicesPaid, paymentsUpdated, errors });
}
