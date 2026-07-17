# fleetdeck admin — Razorpay pay-in panel

Password-protected panel at **admin.fleetdeck.in**: enter an amount, pay via
Razorpay checkout, and see the full payment history with server-confirmed
statuses.

Status lifecycle: `created` (order made) → `paid` (browser signature verified
+ Razorpay API checked) → `captured` (webhook S2S confirmation — the source of
truth) · `failed` (declined / signature mismatch).

## Deploy (Vercel, ~10 minutes)

1. **Neon** (history DB): neon.tech → new project `fleetdeck-admin` → copy the
   connection string.
2. **Razorpay keys**: dashboard.razorpay.com → Account & Settings → API Keys →
   generate → copy Key ID + Key Secret. (Use *live* keys for real money,
   *test* keys to rehearse.)
3. **Vercel**: vercel.com → Add New Project → import `farhanMeows/fleet` →
   set **Root Directory = `admin`** → add env vars:
   - `ADMIN_PASSWORD` — a long random string
   - `RAZORPAY_KEY_ID` / `RAZORPAY_KEY_SECRET`
   - `RAZORPAY_WEBHOOK_SECRET` — invent a long random string (used in step 5)
   - `DATABASE_URL` — the Neon connection string
   - `GOOGLE_CLIENT_ID` — for verifying website sign-ins (see below)
   → Deploy.
4. **Domain**: Vercel project → Settings → Domains → add
   `admin.fleetdeck.in`. In GoDaddy DNS add: CNAME `admin` →
   `cname.vercel-dns.com`.
5. **Webhook (S2S)**: Razorpay dashboard → Account & Settings → Webhooks →
   Add: URL `https://admin.fleetdeck.in/api/webhook`, secret = the
   `RAZORPAY_WEBHOOK_SECRET` you set, events: `payment.captured`,
   `payment.failed`.

## Local dev

```sh
cd admin && npm install
ADMIN_PASSWORD=x RAZORPAY_KEY_ID=rzp_test_… RAZORPAY_KEY_SECRET=… \
RAZORPAY_WEBHOOK_SECRET=x DATABASE_URL=postgres://… npm run dev
# → http://localhost:3999/admin
```

## Caution

Repeatedly paying your own merchant account with your own **credit card**
("self-swiping") violates card-network rules and gets Razorpay accounts
frozen. Small UPI/netbanking amounts for seeding/testing are the sane use.
