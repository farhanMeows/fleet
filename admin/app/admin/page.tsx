"use client";

import { useCallback, useEffect, useState } from "react";

type Payment = {
  id: number;
  order_id: string;
  payment_id: string | null;
  amount_paise: number;
  status: string;
  method: string | null;
  note: string | null;
  created_at: string;
};

type Invoice = {
  number: string;
  bill_to: { name: string } | null;
  usd_subtotal: string;
  inr_total: number;
  status: string;
  pay_link_url: string | null;
};

declare global {
  interface Window {
    Razorpay: new (opts: Record<string, unknown>) => { open: () => void };
  }
}

export default function AdminPage() {
  const [authed, setAuthed] = useState<boolean | null>(null);
  const [password, setPassword] = useState("");
  const [amount, setAmount] = useState("");
  const [note, setNote] = useState("");
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState<{ text: string; isErr: boolean } | null>(null);
  const [payments, setPayments] = useState<Payment[]>([]);

  const [invUsd, setInvUsd] = useState("");
  const [invName, setInvName] = useState("");
  const [invAddr, setInvAddr] = useState("");
  const [invEmail, setInvEmail] = useState("");
  const [invProjects, setInvProjects] = useState("");
  const [invBusy, setInvBusy] = useState(false);
  const [invMsg, setInvMsg] = useState<{ text: string; isErr: boolean } | null>(null);
  const [invoices, setInvoices] = useState<Invoice[]>([]);
  const [syncBusy, setSyncBusy] = useState(false);
  const [syncMsg, setSyncMsg] = useState<{ text: string; isErr: boolean } | null>(null);

  const loadPayments = useCallback(async () => {
    const res = await fetch("/api/payments");
    if (res.status === 401) return setAuthed(false);
    setAuthed(true);
    const data = await res.json();
    setPayments(data.payments ?? []);
    const inv = await fetch("/api/invoice");
    if (inv.ok) setInvoices((await inv.json()).invoices ?? []);
  }, []);

  async function createInvoice(e: React.FormEvent) {
    e.preventDefault();
    setInvMsg(null);
    setInvBusy(true);
    try {
      const res = await fetch("/api/invoice", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          usd: Number(invUsd),
          projects: invProjects ? Number(invProjects) : undefined,
          billTo: invName
            ? {
                name: invName,
                lines: invAddr
                  .split("\n")
                  .map((l) => l.trim())
                  .filter(Boolean),
                email: invEmail.trim() || undefined,
              }
            : undefined,
        }),
      });
      const data = await res.json();
      if (!res.ok) throw new Error(data.error ?? "failed");
      setInvUsd("");
      setInvName("");
      setInvAddr("");
      setInvEmail("");
      setInvProjects("");
      setInvMsg({ text: `created ${data.invoice.number}`, isErr: false });
      await loadPayments();
      window.open(`/invoice/${data.invoice.number}`, "_blank");
    } catch (err) {
      setInvMsg({ text: err instanceof Error ? err.message : "failed", isErr: true });
    } finally {
      setInvBusy(false);
    }
  }

  async function syncRazorpay() {
    setSyncMsg(null);
    setSyncBusy(true);
    try {
      const res = await fetch("/api/reconcile", { method: "POST" });
      const data = await res.json();
      if (!res.ok) throw new Error(data.error ?? "sync failed");
      const errs = data.errors?.length ? ` · ${data.errors.length} lookup(s) failed` : "";
      setSyncMsg({
        text: `synced — ${data.invoicesPaid} invoice(s) marked paid, ${data.paymentsUpdated} payment(s) updated${errs}`,
        isErr: false,
      });
      await loadPayments();
    } catch (err) {
      setSyncMsg({ text: err instanceof Error ? err.message : "sync failed", isErr: true });
    } finally {
      setSyncBusy(false);
    }
  }

  useEffect(() => {
    loadPayments();
    const script = document.createElement("script");
    script.src = "https://checkout.razorpay.com/v1/checkout.js";
    document.body.appendChild(script);
    const t = setInterval(loadPayments, 15000);
    return () => clearInterval(t);
  }, [loadPayments]);

  async function login(e: React.FormEvent) {
    e.preventDefault();
    setMsg(null);
    const res = await fetch("/api/login", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ password }),
    });
    if (!res.ok) return setMsg({ text: "wrong password", isErr: true });
    setPassword("");
    await loadPayments();
  }

  async function pay(e: React.FormEvent) {
    e.preventDefault();
    setMsg(null);
    setBusy(true);
    try {
      const res = await fetch("/api/order", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ amount: Number(amount), note: note || undefined }),
      });
      const data = await res.json();
      if (!res.ok) throw new Error(data.error ?? "order failed");

      new window.Razorpay({
        key: data.keyId,
        order_id: data.orderId,
        amount: data.amount,
        currency: data.currency,
        name: "fleetdeck",
        description: note || "admin top-up",
        theme: { color: "#3fb950" },
        handler: async (resp: Record<string, string>) => {
          const v = await fetch("/api/verify", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({
              orderId: resp.razorpay_order_id,
              paymentId: resp.razorpay_payment_id,
              signature: resp.razorpay_signature,
            }),
          });
          const vd = await v.json();
          setMsg(
            v.ok
              ? { text: `payment ${vd.status} ✓`, isErr: false }
              : { text: vd.error ?? "verification failed", isErr: true },
          );
          loadPayments();
        },
        modal: { ondismiss: () => loadPayments() },
      }).open();
    } catch (err) {
      setMsg({ text: err instanceof Error ? err.message : "failed", isErr: true });
    } finally {
      setBusy(false);
    }
  }

  async function logout() {
    await fetch("/api/logout", { method: "POST" });
    setAuthed(false);
    setPayments([]);
  }

  if (authed === null) return <main className="wrap">…</main>;

  if (!authed) {
    return (
      <main className="wrap">
        <div className="brand">
          fleetdeck <span>/ admin</span>
        </div>
        <form className="panel" onSubmit={login}>
          <h2>LOGIN</h2>
          <div className="row">
            <input
              type="password"
              placeholder="admin password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              autoFocus
            />
            <button className="primary" type="submit">
              enter
            </button>
          </div>
          {msg && <div className={msg.isErr ? "err" : "ok"}>{msg.text}</div>}
        </form>
      </main>
    );
  }

  return (
    <main className="wrap">
      <div className="row" style={{ justifyContent: "space-between" }}>
        <div className="brand">
          fleetdeck <span>/ admin</span>
        </div>
        <button className="ghost" onClick={logout}>
          logout
        </button>
      </div>

      <form className="panel" onSubmit={pay}>
        <h2>PAY</h2>
        <div className="row">
          <span style={{ color: "var(--dim)" }}>₹</span>
          <input
            className="amount"
            type="number"
            min="1"
            step="0.01"
            placeholder="amount (INR)"
            value={amount}
            onChange={(e) => setAmount(e.target.value)}
            required
          />
          <input
            placeholder="note (optional)"
            value={note}
            onChange={(e) => setNote(e.target.value)}
          />
          <button className="primary" type="submit" disabled={busy}>
            {busy ? "…" : "pay →"}
          </button>
        </div>
        {msg && <div className={msg.isErr ? "err" : "ok"}>{msg.text}</div>}
        <div className="hint">
          created → paid (browser-verified) → captured (webhook-confirmed). failed = declined or
          signature mismatch.
        </div>
      </form>

      <form className="panel" onSubmit={createInvoice}>
        <h2>CREATE INVOICE</h2>
        <div className="row">
          <span style={{ color: "var(--dim)" }}>$</span>
          <input
            className="amount"
            type="number"
            min="0.01"
            step="0.01"
            placeholder="amount (USD, decimals ok)"
            value={invUsd}
            onChange={(e) => setInvUsd(e.target.value)}
            required
          />
          <input
            placeholder="bill-to name (blank = Farhan's projects)"
            value={invName}
            onChange={(e) => setInvName(e.target.value)}
            style={{ flex: 1, minWidth: 180 }}
          />
          <button className="primary" type="submit" disabled={invBusy}>
            {invBusy ? "…" : "create invoice →"}
          </button>
        </div>
        <div className="row">
          <textarea
            placeholder={"bill-to address (one line per row)"}
            value={invAddr}
            onChange={(e) => setInvAddr(e.target.value)}
            rows={2}
            style={{ flex: 2, minWidth: 220, resize: "vertical" }}
          />
          <input
            type="email"
            placeholder="bill-to email"
            value={invEmail}
            onChange={(e) => setInvEmail(e.target.value)}
            style={{ flex: 1, minWidth: 180 }}
          />
          <input
            type="number"
            min="1"
            max="999"
            step="1"
            placeholder="active projects (blank = auto)"
            value={invProjects}
            onChange={(e) => setInvProjects(e.target.value)}
            style={{ width: 220 }}
          />
        </div>
        {invMsg && <div className={invMsg.isErr ? "err" : "ok"}>{invMsg.text}</div>}
        <div className="hint">
          Invoice shows USD only (GST added on top); the attached Razorpay pay-link converts to INR
          at today&rsquo;s rate. Opens the printable invoice; status flips to paid when the link is
          settled. Address &amp; email print in the invoice&rsquo;s Bill-to block.
        </div>
      </form>

      <div className="panel">
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "baseline" }}>
          <h2>INVOICES</h2>
          <button onClick={syncRazorpay} disabled={syncBusy} title="Re-check pending invoices & payments against Razorpay">
            {syncBusy ? "syncing…" : "sync with razorpay"}
          </button>
        </div>
        {syncMsg && <div className={syncMsg.isErr ? "err" : "ok"}>{syncMsg.text}</div>}
        {invoices.length === 0 ? (
          <div className="hint">no invoices yet</div>
        ) : (
          <table>
            <thead>
              <tr>
                <th>NUMBER</th>
                <th>BILL TO</th>
                <th>USD</th>
                <th>INR TOTAL</th>
                <th>STATUS</th>
                <th>ACTIONS</th>
              </tr>
            </thead>
            <tbody>
              {invoices.map((iv) => (
                <tr key={iv.number}>
                  <td className="mono">{iv.number}</td>
                  <td>{iv.bill_to?.name ?? "—"}</td>
                  <td>${Number(iv.usd_subtotal).toLocaleString()}</td>
                  <td>₹{(iv.inr_total / 100).toLocaleString("en-IN")}</td>
                  <td>
                    <span className={`st ${iv.status}`}>{iv.status}</span>
                  </td>
                  <td className="mono">
                    <a href={`/invoice/${iv.number}`} target="_blank" rel="noreferrer">
                      view
                    </a>
                    {iv.pay_link_url && iv.status !== "paid" && (
                      <>
                        {" · "}
                        <a href={iv.pay_link_url} target="_blank" rel="noreferrer">
                          pay
                        </a>
                        {" · "}
                        <a
                          href="#"
                          onClick={(e) => {
                            e.preventDefault();
                            navigator.clipboard?.writeText(iv.pay_link_url!);
                            setInvMsg({ text: `copied pay link for ${iv.number}`, isErr: false });
                          }}
                        >
                          copy link
                        </a>
                      </>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      <div className="panel">
        <h2>PAYMENTS</h2>
        {payments.length === 0 ? (
          <div className="hint">no payments yet</div>
        ) : (
          <table>
            <thead>
              <tr>
                <th>WHEN</th>
                <th>AMOUNT</th>
                <th>STATUS</th>
                <th>METHOD</th>
                <th>NOTE</th>
                <th>IDS</th>
              </tr>
            </thead>
            <tbody>
              {payments.map((p) => (
                <tr key={p.id}>
                  <td className="mono">{new Date(p.created_at).toLocaleString()}</td>
                  <td>₹{(p.amount_paise / 100).toLocaleString("en-IN")}</td>
                  <td>
                    <span className={`st ${p.status}`}>{p.status}</span>
                  </td>
                  <td className="mono">{p.method ?? "—"}</td>
                  <td className="mono">{p.note ?? "—"}</td>
                  <td className="mono">
                    {p.order_id}
                    {p.payment_id ? ` · ${p.payment_id}` : ""}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </main>
  );
}
