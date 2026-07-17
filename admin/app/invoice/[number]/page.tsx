import { notFound } from "next/navigation";
import { getInvoiceByNumber } from "@/lib/db";
import { SELLER } from "@/lib/seller";
import PrintBar from "./print-bar";
import "./invoice.css";

function usd(n: number): string {
  return "$" + n.toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 2 });
}
function date(s: string): string {
  return new Date(s).toLocaleDateString("en-US", { month: "long", day: "numeric", year: "numeric" });
}

export default async function InvoicePage({ params }: { params: Promise<{ number: string }> }) {
  const { number } = await params;
  const inv = await getInvoiceByNumber(number);
  if (!inv) notFound();

  const usdSub = Number(inv.usd_subtotal);
  const gstRate = Number(inv.gst_rate);
  const gstPct = Math.round(gstRate * 100);
  const usdGst = Math.round(usdSub * gstRate * 100) / 100;
  const usdTotal = Math.round((usdSub + usdGst) * 100) / 100;

  return (
    <div className="inv-page">
      <div className="inv">
        <header className="inv-top">
          <h1>Invoice</h1>
        </header>

        <div className="inv-meta">
          <div>
            <span>Invoice number</span>
            <b>{inv.number}</b>
          </div>
          <div>
            <span>Date of issue</span>
            <b>{date(inv.issued_at)}</b>
          </div>
          <div>
            <span>Status</span>
            <b className={`inv-status ${inv.status}`}>{inv.status.toUpperCase()}</b>
          </div>
        </div>

        <div className="inv-parties">
          <div>
            <b>{SELLER.name}</b>
            {SELLER.addressLines.map((l) => (
              <div key={l}>{l}</div>
            ))}
          </div>
          <div>
            <b>Bill to</b>
            <div>{inv.bill_to.name}</div>
            {inv.bill_to.lines.map((l) => (
              <div key={l}>{l}</div>
            ))}
            {inv.bill_to.email && <div>{inv.bill_to.email}</div>}
          </div>
        </div>

        <div className="inv-due">
          <h2>{usd(usdTotal)} due {date(inv.issued_at)}</h2>
          {inv.pay_link_url && inv.status !== "paid" && (
            <a className="inv-pay" href={inv.pay_link_url}>
              Pay online
            </a>
          )}
        </div>

        {SELLER.gstin ? (
          <p className="inv-tax-note">GSTIN: {SELLER.gstin}</p>
        ) : (
          <p className="inv-tax-note">{SELLER.note}</p>
        )}
        <table className="inv-lines">
          <thead>
            <tr>
              <th>Description</th>
              <th className="r">Qty</th>
              <th className="r">Unit price</th>
              <th className="r">Amount</th>
            </tr>
          </thead>
          <tbody>
            {inv.items.map((it, i) => (
              <tr key={i}>
                <td>
                  {it.description}
                  {it.detail && <div className="inv-detail">{it.detail}</div>}
                </td>
                <td className="r">{it.qty}</td>
                <td className="r">{usd(it.usdUnit)}</td>
                <td className="r">{usd(it.qty * it.usdUnit)}</td>
              </tr>
            ))}
          </tbody>
        </table>

        <div className="inv-totals">
          <div>
            <span>Subtotal</span>
            <span>{usd(usdSub)}</span>
          </div>
          <div>
            <span>IGST — INDIA ({gstPct}%)</span>
            <span>{usd(usdGst)}</span>
          </div>
          <div className="inv-total-row">
            <span>Total</span>
            <span>{usd(usdTotal)}</span>
          </div>
          <div className="inv-total-row">
            <span>Amount due</span>
            <span>{usd(usdTotal)}</span>
          </div>
        </div>

        <footer className="inv-foot">
          Questions about this invoice? Contact {SELLER.email}.
        </footer>
      </div>
      <PrintBar />
    </div>
  );
}
