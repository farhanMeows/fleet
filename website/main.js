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

// Copy buttons — re-runnable so dynamically injected blocks get wired too.
function bindCopy(root = document) {
  root.querySelectorAll(".codeblock").forEach((block) => {
    const btn = block.querySelector(".copy");
    if (!btn || btn.dataset.bound) return;
    btn.dataset.bound = "1";
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
}
bindCopy();

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
const terminalEl = document.getElementById("terminal");
if (terminalEl) {
  const termIO = new IntersectionObserver((entries) => {
    if (entries.some((e) => e.isIntersecting)) {
      termIO.disconnect();
      setTimeout(type, 500);
    }
  });
  termIO.observe(terminalEl);
}

/* ————— account session & gated install —————
   Sign-in happens on signin.html / signup.html (Google only). The admin API
   returns a signed download token; the install command below embeds it, and
   the download endpoints reject requests without one. Logged out, this page
   contains no install command at all. */

const ADMIN_API = "https://admin.fleetdeck.in";
const SESSION_KEY = "fleetdeck_session";

const lockedEl = document.getElementById("install-locked");
const stepsEl = document.getElementById("install-steps");
const signedEl = document.getElementById("install-signed");
const noteEl = document.getElementById("install-note");
const navAuthEl = document.getElementById("nav-auth");

function session() {
  try {
    const s = JSON.parse(localStorage.getItem(SESSION_KEY) || "null");
    return s?.token && (!s.exp || Date.now() < s.exp) ? s : null;
  } catch {
    return null;
  }
}

function esc(s) {
  return String(s).replace(/[&<>"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]));
}

function renderSteps(user) {
  const curl = `curl -fsSL "${ADMIN_API}/api/install.sh?t=${user.token}" | sh`;
  stepsEl.innerHTML = `
    <div class="step glass in">
      <div class="step-head"><span class="step-n">1</span><span>Install fleet — your personal one-liner, picks the right binary for your Mac</span></div>
      <div class="codeblock wrap" data-copy="${esc(curl)}">
        <pre><code><span class="c-dollar">$</span> <span class="c-cmd">curl</span> -fsSL "${esc(ADMIN_API)}/api/install.sh?t=${esc(user.token)}" <span class="c-op">|</span> <span class="c-cmd">sh</span></code></pre>
        <button class="copy" aria-label="Copy command">Copy</button>
      </div>
    </div>

    <div class="step glass in">
      <div class="step-head"><span class="step-n">2</span><span>Wire up Claude Code hooks — idempotent, backs up your settings first</span></div>
      <div class="codeblock" data-copy="fleet install">
        <pre><code><span class="c-dollar">$</span> <span class="c-cmd">fleet</span> install</code></pre>
        <button class="copy" aria-label="Copy command">Copy</button>
      </div>
    </div>

    <div class="step glass in">
      <div class="step-head"><span class="step-n">3</span><span>Register the projects you want on the grid</span></div>
      <div class="codeblock" data-copy="fleet add ~/code/my-project">
        <pre><code><span class="c-dollar">$</span> <span class="c-cmd">fleet</span> add ~/code/my-project
<span class="c-dollar">$</span> <span class="c-cmd">fleet</span> add ~/code/another-one</code></pre>
        <button class="copy" aria-label="Copy command">Copy</button>
      </div>
    </div>

    <div class="step glass in">
      <div class="step-head"><span class="step-n">4</span><span>Launch mission control</span></div>
      <div class="codeblock" data-copy="fleet up">
        <pre><code><span class="c-dollar">$</span> <span class="c-cmd">fleet</span> up   <span class="c-comment"># tmux session · dashboard + one claude window per project</span></code></pre>
        <button class="copy" aria-label="Copy command">Copy</button>
      </div>
    </div>`;
  bindCopy(stepsEl);
}

function renderAuthState() {
  const user = session();
  if (user) {
    lockedEl.hidden = true;
    stepsEl.hidden = false;
    noteEl.hidden = false;
    document.getElementById("signed-email").textContent = user.email || "your account";
    signedEl.hidden = false;
    renderSteps(user);
    if (navAuthEl) {
      navAuthEl.textContent = "Install";
      navAuthEl.href = "#install";
    }
    // Get-started CTAs jump straight to install once signed in.
    document.querySelectorAll("a[data-cta]").forEach((a) => (a.href = "#install"));
  } else {
    lockedEl.hidden = false;
    stepsEl.hidden = true;
    stepsEl.innerHTML = "";
    noteEl.hidden = true;
    signedEl.hidden = true;
  }
}

document.getElementById("sign-out")?.addEventListener("click", (e) => {
  e.preventDefault();
  localStorage.removeItem(SESSION_KEY);
  renderAuthState();
});

renderAuthState();
