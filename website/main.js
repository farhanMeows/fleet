/* ————— fleet marketing site ————— */

// Sticky nav background on scroll.
const nav = document.getElementById("nav");
const onScroll = () => nav.classList.toggle("scrolled", window.scrollY > 24);
onScroll();
addEventListener("scroll", onScroll, { passive: true });

// Reveal-on-scroll.
const io = new IntersectionObserver(
  (entries) => {
    for (const e of entries) {
      if (e.isIntersecting) {
        e.target.classList.add("in");
        io.unobserve(e.target);
      }
    }
  },
  { threshold: 0.12, rootMargin: "0px 0px -40px 0px" }
);
document.querySelectorAll(".reveal").forEach((el) => io.observe(el));

// Cursor-tracking glow on feature cards.
document.querySelectorAll(".card").forEach((card) => {
  card.addEventListener("pointermove", (e) => {
    const r = card.getBoundingClientRect();
    card.style.setProperty("--mx", `${e.clientX - r.left}px`);
    card.style.setProperty("--my", `${e.clientY - r.top}px`);
  });
});

// Copy buttons.
document.querySelectorAll(".codeblock").forEach((block) => {
  const btn = block.querySelector(".copy");
  if (!btn) return;
  btn.addEventListener("click", async () => {
    const text = block.dataset.copy || block.querySelector("code").innerText;
    try {
      await navigator.clipboard.writeText(text);
      btn.textContent = "Copied";
      btn.classList.add("done");
      setTimeout(() => {
        btn.textContent = "Copy";
        btn.classList.remove("done");
      }, 1600);
    } catch {
      /* clipboard unavailable (e.g. non-secure context) — no-op */
    }
  });
});

/* ————— animated hero terminal ————— */

const typedEl = document.getElementById("typed");
const cursorEl = document.getElementById("cursor");
const dashEl = document.getElementById("dash");
const clockEl = document.getElementById("clock");
const rows = [...document.querySelectorAll("[data-row]")];

const STATES = {
  working: { icon: "●", cls: "st-working", label: "working" },
  needs:   { icon: "⚠", cls: "st-needs",   label: "NEEDS YOU" },
  idle:    { icon: "✓", cls: "st-idle",    label: "idle" },
  none:    { icon: "○", cls: "st-none",    label: "no session" },
};

const NOW = {
  working: [
    "Bash: npm test",
    "Edit: api/orders.ts",
    "Bash: go vet ./...",
    "Read: internal/store/db.go",
    "Bash: make build",
    "Write: src/routes/billing.tsx",
    "Bash: pytest -x",
    "Grep: TODO(auth)",
  ],
  needs: [
    "Bash: pm2 restart api",
    "Bash: git push origin main",
    "Bash: rm -rf dist/",
    "Bash: npm publish",
  ],
};

// Initial board — mirrors the README mockup.
const board = ["working", "needs", "working", "idle", "none"];

function paintRow(el, stateKey, flip = false) {
  const s = STATES[stateKey];
  el.querySelector(".t-ic").textContent = s.icon;
  const st = el.querySelector(".t-state");
  st.textContent = s.label;
  st.className = "t-state " + s.cls;
  el.querySelector(".t-ic").className = "t-ic " + s.cls;
  const pool = NOW[stateKey];
  el.querySelector(".t-now").textContent = pool
    ? pool[Math.floor(Math.random() * pool.length)]
    : "—";
  if (flip) {
    el.classList.remove("flip");
    void el.offsetWidth; // restart animation
    el.classList.add("flip");
  }
}

function tickClock() {
  clockEl.textContent = new Date().toTimeString().slice(0, 8);
}

// Type "fleet up", then reveal the dashboard and start mutating states.
const CMD = "fleet up";
let i = 0;
function type() {
  if (i <= CMD.length) {
    typedEl.textContent = CMD.slice(0, i);
    i++;
    setTimeout(type, 70 + Math.random() * 90);
    return;
  }
  setTimeout(() => {
    cursorEl.remove();
    dashEl.hidden = false;
    rows.forEach((r, n) => paintRow(r, board[n]));
    tickClock();
    setInterval(tickClock, 1000);
    setInterval(mutate, 2400);
  }, 420);
}

// Random walk: one row changes state per tick, keeping the board lively.
const NEXT = {
  working: ["idle", "needs", "working"],
  needs: ["working"],
  idle: ["working", "idle"],
  none: ["working"],
};
function mutate() {
  const n = Math.floor(Math.random() * rows.length);
  const options = NEXT[board[n]];
  board[n] = options[Math.floor(Math.random() * options.length)];
  paintRow(rows[n], board[n], true);
}

// Start typing once the terminal scrolls into view (or immediately if visible).
const termIO = new IntersectionObserver((entries) => {
  if (entries.some((e) => e.isIntersecting)) {
    termIO.disconnect();
    setTimeout(type, 500);
  }
});
termIO.observe(document.getElementById("terminal"));
