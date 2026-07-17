import { Pool } from "pg";

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

// Aiven Postgres: TLS is mandatory but the CA is Aiven's own, so verification
// must be disabled (same pattern as other Aiven-backed apps). The sslmode
// query param is stripped because node-postgres would otherwise try (and
// fail) to verify the certificate chain.
let pool: Pool | null = null;

function db(): Pool {
  if (pool) return pool;
  const url = process.env.DATABASE_URL;
  if (!url) throw new Error("DATABASE_URL env var is not set");
  pool = new Pool({
    connectionString: url.replace(/[?&]sslmode=[^&]+/, ""),
    ssl: { rejectUnauthorized: false },
    max: 3,
  });
  return pool;
}

async function q<T = unknown>(text: string, params: unknown[] = []): Promise<T[]> {
  const res = await db().query(text, params);
  return res.rows as T[];
}

let ensured = false;

export async function ensureSchema(): Promise<void> {
  if (ensured) return;
  await q(`
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
    )`);
  await q(`
    CREATE TABLE IF NOT EXISTS invoices (
      id             SERIAL PRIMARY KEY,
      number         TEXT NOT NULL UNIQUE,
      bill_to        JSONB NOT NULL,
      items          JSONB NOT NULL,
      usd_subtotal   NUMERIC(12,2) NOT NULL,
      fx_rate        NUMERIC(10,4) NOT NULL,
      inr_subtotal   BIGINT NOT NULL,
      gst_rate       NUMERIC(4,3) NOT NULL,
      inr_gst        BIGINT NOT NULL,
      inr_total      BIGINT NOT NULL,
      status         TEXT NOT NULL DEFAULT 'draft',
      pay_link_id    TEXT,
      pay_link_url   TEXT,
      payment_id     TEXT,
      issued_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
      paid_at        TIMESTAMPTZ
    )`);
  await q(`
    CREATE TABLE IF NOT EXISTS users (
      id         SERIAL PRIMARY KEY,
      sub        TEXT NOT NULL UNIQUE,
      email      TEXT NOT NULL,
      name       TEXT,
      sign_ins   INTEGER NOT NULL DEFAULT 1,
      first_seen TIMESTAMPTZ NOT NULL DEFAULT now(),
      last_seen  TIMESTAMPTZ NOT NULL DEFAULT now()
    )`);
  ensured = true;
}

export async function insertOrder(orderId: string, amountPaise: number, note: string | null) {
  await ensureSchema();
  await q(
    `INSERT INTO payments (order_id, amount_paise, note) VALUES ($1, $2, $3)
     ON CONFLICT (order_id) DO NOTHING`,
    [orderId, amountPaise, note],
  );
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
  const rows = await q<{ status: string }>(`SELECT status FROM payments WHERE order_id = $1`, [
    opts.orderId,
  ]);
  const current = rows[0]?.status ?? "created";
  const next = (rank[opts.status] ?? 0) >= (rank[current] ?? 0) ? opts.status : current;
  await q(
    `INSERT INTO payments (order_id, payment_id, amount_paise, status, method)
     VALUES ($1, $2, $3, $4, $5)
     ON CONFLICT (order_id) DO UPDATE SET
       payment_id = COALESCE(EXCLUDED.payment_id, payments.payment_id),
       status     = $4,
       method     = COALESCE(EXCLUDED.method, payments.method),
       updated_at = now()`,
    [opts.orderId, opts.paymentId ?? null, opts.amountPaise ?? 0, next, opts.method ?? null],
  );
}

export async function upsertUser(u: { sub: string; email: string; name: string | null }) {
  await ensureSchema();
  await q(
    `INSERT INTO users (sub, email, name) VALUES ($1, $2, $3)
     ON CONFLICT (sub) DO UPDATE SET
       email = EXCLUDED.email,
       name = COALESCE(EXCLUDED.name, users.name),
       sign_ins = users.sign_ins + 1,
       last_seen = now()`,
    [u.sub, u.email, u.name],
  );
}

export async function listPayments(limit = 50): Promise<PaymentRow[]> {
  await ensureSchema();
  return q<PaymentRow>(`SELECT * FROM payments ORDER BY created_at DESC LIMIT $1`, [limit]);
}

// --- invoices ---

export type BillTo = { name: string; lines: string[]; email?: string };
export type InvoiceItem = { description: string; detail?: string; qty: number; usdUnit: number };

export type InvoiceRow = {
  id: number;
  number: string;
  bill_to: BillTo;
  items: InvoiceItem[];
  usd_subtotal: string;
  fx_rate: string;
  inr_subtotal: number;
  gst_rate: string;
  inr_gst: number;
  inr_total: number;
  status: string; // draft | sent | paid | failed
  pay_link_id: string | null;
  pay_link_url: string | null;
  payment_id: string | null;
  issued_at: string;
  paid_at: string | null;
};

// nextInvoiceNumber: FD-YYYY-NNNN, per-year sequence.
export async function nextInvoiceNumber(year: number): Promise<string> {
  await ensureSchema();
  const rows = await q<{ number: string }>(
    `SELECT number FROM invoices WHERE number LIKE $1 ORDER BY id DESC LIMIT 1`,
    [`FD-${year}-%`],
  );
  const last = rows[0]?.number?.split("-")[2];
  const next = (last ? parseInt(last, 10) : 0) + 1;
  return `FD-${year}-${String(next).padStart(4, "0")}`;
}

export async function insertInvoice(inv: {
  number: string;
  billTo: BillTo;
  items: InvoiceItem[];
  usdSubtotal: number;
  fxRate: number;
  inrSubtotal: number;
  gstRate: number;
  inrGst: number;
  inrTotal: number;
  payLinkId?: string;
  payLinkUrl?: string;
  status: string;
}): Promise<InvoiceRow> {
  await ensureSchema();
  const rows = await q<InvoiceRow>(
    `INSERT INTO invoices
       (number, bill_to, items, usd_subtotal, fx_rate, inr_subtotal, gst_rate, inr_gst, inr_total, pay_link_id, pay_link_url, status)
     VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
     RETURNING *`,
    [
      inv.number,
      JSON.stringify(inv.billTo),
      JSON.stringify(inv.items),
      inv.usdSubtotal,
      inv.fxRate,
      inv.inrSubtotal,
      inv.gstRate,
      inv.inrGst,
      inv.inrTotal,
      inv.payLinkId ?? null,
      inv.payLinkUrl ?? null,
      inv.status,
    ],
  );
  return rows[0];
}

export async function getInvoiceByNumber(number: string): Promise<InvoiceRow | null> {
  await ensureSchema();
  const rows = await q<InvoiceRow>(`SELECT * FROM invoices WHERE number = $1`, [number]);
  return rows[0] ?? null;
}

export async function markInvoicePaid(payLinkId: string, paymentId: string) {
  await ensureSchema();
  await q(
    `UPDATE invoices SET status = 'paid', payment_id = $2, paid_at = now()
     WHERE pay_link_id = $1 AND status != 'paid'`,
    [payLinkId, paymentId],
  );
}

export async function listInvoices(limit = 50): Promise<InvoiceRow[]> {
  await ensureSchema();
  return q<InvoiceRow>(`SELECT * FROM invoices ORDER BY issued_at DESC LIMIT $1`, [limit]);
}
