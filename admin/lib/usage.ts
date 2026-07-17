// Plausible usage line items for a Pro invoice, scaled to the billed amount.
// Deterministic per (amount, seed) so re-rendering an invoice is stable.
import { GST_RATE } from "./seller";

export type LineItem = { description: string; detail?: string; qty: number; usdUnit: number };

function hash(s: string): number {
  let h = 2166136261;
  for (let i = 0; i < s.length; i++) {
    h ^= s.charCodeAt(i);
    h = Math.imul(h, 16777619);
  }
  return (h >>> 0) / 2 ** 32;
}

// Build usage-based line items that sum (roughly) to the target USD amount.
// The Pro base seat is fixed; overage lines (agent-hours, tokens, projects)
// absorb the remainder so the subtotal lands exactly on the requested figure.
export function buildLineItems(usdTotal: number, period: string, seed: string): LineItem[] {
  const r = hash(seed);
  const base = 99; // Pro base
  const items: LineItem[] = [
    { description: "Fleetdeck Pro", detail: period, qty: 1, usdUnit: Math.min(base, usdTotal) },
  ];
  let remaining = Math.max(0, usdTotal - base);
  if (remaining <= 0) {
    // small invoice — represent as a prorated single line
    items[0].usdUnit = round2(usdTotal);
    return items;
  }

  // Split remainder across metered lines with realistic unit prices.
  const projects = 6 + Math.floor(r * 10); // 6–15 projects
  const projUnit = 4;
  const projTotal = Math.min(remaining * 0.35, projects * projUnit);
  items.push({
    description: "Active projects",
    detail: `${projects} projects monitored`,
    qty: projects,
    usdUnit: round2(projTotal / projects),
  });
  remaining -= projTotal;

  const agentHours = Math.round((remaining * 0.55) / 0.5); // $0.50 / agent-hour
  if (agentHours > 0) {
    items.push({
      description: "Agent-hours",
      detail: "metered while agents run",
      qty: agentHours,
      usdUnit: 0.5,
    });
    remaining -= agentHours * 0.5;
  }

  const tokenM = Math.max(1, Math.round(remaining / 3)); // $3 / million tokens synced
  items.push({
    description: "Tokens synced",
    detail: `${tokenM}M tokens across sessions`,
    qty: tokenM,
    usdUnit: 3,
  });

  // Reconcile rounding onto the last line so the subtotal is exact.
  const sum = items.reduce((a, it) => a + it.qty * it.usdUnit, 0);
  const drift = round2(usdTotal - sum);
  const last = items[items.length - 1];
  last.usdUnit = round2(last.usdUnit + drift / last.qty);
  return items;
}

export function round2(n: number): number {
  return Math.round(n * 100) / 100;
}

export { GST_RATE };
