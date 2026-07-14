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

  const loadPayments = useCallback(async () => {
    const res = await fetch("/api/payments");
    if (res.status === 401) return setAuthed(false);
    setAuthed(true);
    const data = await res.json();
    setPayments(data.payments ?? []);
  }, []);

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

      <div className="panel">
        <h2>HISTORY</h2>
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
