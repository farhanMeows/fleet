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
