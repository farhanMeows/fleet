import { NextRequest, NextResponse } from "next/server";
import Razorpay from "razorpay";
import { isAuthed } from "@/lib/auth";
import { insertInvoice, nextInvoiceNumber, listInvoices, type BillTo } from "@/lib/db";
import { usdInr } from "@/lib/fx";
import { buildLineItems, round2 } from "@/lib/usage";
import { GST_RATE } from "@/lib/seller";

const DEFAULT_BILL_TO: BillTo = {
  name: "Farhan's projects",
  lines: ["Paona Bazar, Imphal 795001", "Imphal 795001", "MANIPUR", "India"],
  email: "farhanzakiahmad2017@gmail.com",
};

export async function GET() {
  if (!(await isAuthed())) return NextResponse.json({ error: "unauthorized" }, { status: 401 });
  return NextResponse.json({ invoices: await listInvoices() });
}

export async function POST(req: NextRequest) {
  if (!(await isAuthed())) return NextResponse.json({ error: "unauthorized" }, { status: 401 });

  const body = await req.json().catch(() => ({}));
  const usd = Number(body.usd);
  if (!Number.isFinite(usd) || usd < 1 || usd > 100000) {
    return NextResponse.json({ error: "amount must be $1 – $100,000" }, { status: 400 });
  }
  const withLink = body.paymentLink !== false; // default: attach a Razorpay link
  const billTo: BillTo = body.billTo?.name ? body.billTo : DEFAULT_BILL_TO;

  const now = new Date();
  const number = await nextInvoiceNumber(now.getUTCFullYear());
  const period = billingPeriod(now);
  const items = buildLineItems(usd, period, number);

  const usdSubtotal = round2(items.reduce((a, it) => a + it.qty * it.usdUnit, 0));
  const { rate } = await usdInr();
  const inrSubtotal = Math.round(usdSubtotal * rate * 100); // paise
  const inrGst = Math.round(inrSubtotal * GST_RATE);
  const inrTotal = inrSubtotal + inrGst;

  let payLinkId: string | undefined;
  let payLinkUrl: string | undefined;
  if (withLink) {
    try {
      const rzp = new Razorpay({
        key_id: process.env.RAZORPAY_KEY_ID!,
        key_secret: process.env.RAZORPAY_KEY_SECRET!,
      });
      const link = await rzp.paymentLink.create({
        amount: inrTotal,
        currency: "INR",
        description: `Fleetdeck invoice ${number}`,
        reference_id: number,
        customer: { name: billTo.name, email: billTo.email },
        notify: { email: false, sms: false },
        reminder_enable: false,
        notes: { invoice: number, usd: String(usdSubtotal) },
      });
      payLinkId = link.id;
      payLinkUrl = link.short_url;
    } catch (e) {
      return NextResponse.json(
        { error: "payment link failed: " + (e instanceof Error ? e.message : "unknown") },
        { status: 502 },
      );
    }
  }

  const invoice = await insertInvoice({
    number,
    billTo,
    items,
    usdSubtotal,
    fxRate: rate,
    inrSubtotal,
    gstRate: GST_RATE,
    inrGst,
    inrTotal,
    payLinkId,
    payLinkUrl,
    status: withLink ? "sent" : "draft",
  });

  return NextResponse.json({ invoice });
}

function billingPeriod(d: Date): string {
  const start = new Date(d);
  const end = new Date(d);
  end.setDate(end.getDate() + 30);
  const fmt = (x: Date) => x.toLocaleDateString("en-US", { month: "short", day: "numeric" });
  return `${fmt(start)} – ${fmt(end)}, ${d.getUTCFullYear()}`;
}
