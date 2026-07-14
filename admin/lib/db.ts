import { neon } from "@neondatabase/serverless";

export type PaymentRow = {
  id: number;
  order_id: string;
  payment_id: string | null;
  amount_paise: number;
  currency: string;
  status: string; // created | paid | captured | failed
  method: string | null;
  note: string | null;
  created_at: string;
  updated_at: string;
};

function sql() {
  const url = process.env.DATABASE_URL;
  if (!url) throw new Error("DATABASE_URL env var is not set");
  return neon(url);
}

let ensured = false;

export async function ensureSchema(): Promise<void> {
  if (ensured) return;
  await sql()`
    CREATE TABLE IF NOT EXISTS payments (
      id            SERIAL PRIMARY KEY,
      order_id      TEXT NOT NULL UNIQUE,
      payment_id    TEXT,
      amount_paise  BIGINT NOT NULL,
      currency      TEXT NOT NULL DEFAULT 'INR',
      status        TEXT NOT NULL DEFAULT 'created',
      method        TEXT,
      note          TEXT,
      created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
      updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
    )`;
  ensured = true;
}

export async function insertOrder(orderId: string, amountPaise: number, note: string | null) {
  await ensureSchema();
  await sql()`
    INSERT INTO payments (order_id, amount_paise, note)
    VALUES (${orderId}, ${amountPaise}, ${note})
    ON CONFLICT (order_id) DO NOTHING`;
}

// markPayment upserts payment state. Status precedence prevents a late
// browser "paid" from downgrading a webhook-confirmed "captured".
const rank: Record<string, number> = { created: 0, failed: 1, paid: 2, captured: 3 };

export async function markPayment(opts: {
  orderId: string;
  paymentId?: string;
  status: string;
  method?: string;
  amountPaise?: number;
}) {
  await ensureSchema();
  const rows = (await sql()`SELECT status FROM payments WHERE order_id = ${opts.orderId}`) as {
    status: string;
  }[];
  const current = rows[0]?.status ?? "created";
  const next = (rank[opts.status] ?? 0) >= (rank[current] ?? 0) ? opts.status : current;
  await sql()`
    INSERT INTO payments (order_id, payment_id, amount_paise, status, method)
    VALUES (${opts.orderId}, ${opts.paymentId ?? null}, ${opts.amountPaise ?? 0}, ${next}, ${opts.method ?? null})
    ON CONFLICT (order_id) DO UPDATE SET
      payment_id = COALESCE(EXCLUDED.payment_id, payments.payment_id),
      status     = ${next},
      method     = COALESCE(EXCLUDED.method, payments.method),
      updated_at = now()`;
}

export async function listPayments(limit = 50): Promise<PaymentRow[]> {
  await ensureSchema();
  return (await sql()`
    SELECT * FROM payments ORDER BY created_at DESC LIMIT ${limit}`) as PaymentRow[];
}
