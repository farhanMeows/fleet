// USD→INR rate with a 12h in-memory cache and a sane fallback so invoice
// creation never blocks on the FX provider.
let cached: { rate: number; at: number } | null = null;
const FALLBACK = 84.0;

export async function usdInr(): Promise<{ rate: number; source: string }> {
  if (cached && Date.now() - cached.at < 12 * 3600 * 1000) {
    return { rate: cached.rate, source: "cache" };
  }
  try {
    const res = await fetch("https://open.er-api.com/v6/latest/USD", {
      signal: AbortSignal.timeout(5000),
    });
    const data = await res.json();
    const rate = Number(data?.rates?.INR);
    if (Number.isFinite(rate) && rate > 50 && rate < 150) {
      cached = { rate, at: Date.now() };
      return { rate, source: "live" };
    }
  } catch {
    /* fall through */
  }
  return { rate: cached?.rate ?? FALLBACK, source: cached ? "stale-cache" : "fallback" };
}
