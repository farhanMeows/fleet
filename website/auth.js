/* ————— fleetdeck sign-in / sign-up pages —————
   Google-only auth. On success the admin API verifies the ID token, records
   the account, and returns a signed download token; we stash it and land the
   user on the install section, which unlocks with their personal command. */

const GOOGLE_CLIENT_ID = "967010490410-esda3g39m59poia2c4qadi0mln0loe3c.apps.googleusercontent.com";
const ACCOUNTS_API = "https://admin.fleetdeck.in/api/auth/google";
const SESSION_KEY = "fleetdeck_session";

const mode = document.body.dataset.mode === "signup" ? "signup" : "signin";
const errEl = document.getElementById("auth-err");

function session() {
  try {
    const s = JSON.parse(localStorage.getItem(SESSION_KEY) || "null");
    return s?.token && (!s.exp || Date.now() < s.exp) ? s : null;
  } catch {
    return null;
  }
}

// Already signed in → straight to the install section.
if (session()) location.replace("./#install");

function fail(text) {
  errEl.textContent = text;
  errEl.hidden = false;
}

async function onCredential(resp) {
  errEl.hidden = true;
  try {
    const res = await fetch(ACCOUNTS_API, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ credential: resp.credential }),
    });
    const data = await res.json().catch(() => ({}));
    if (!res.ok || !data.ok) throw new Error(data.error || "sign-in failed");
    localStorage.setItem(
      SESSION_KEY,
      JSON.stringify({
        email: data.email,
        name: data.name,
        token: data.token,
        exp: data.exp,
        at: Date.now(),
      }),
    );
    location.href = "./#install";
  } catch (e) {
    fail((e && e.message) || "sign-in failed — please try again");
  }
}

function boot() {
  if (!window.google?.accounts?.id) return setTimeout(boot, 200);
  google.accounts.id.initialize({ client_id: GOOGLE_CLIENT_ID, callback: onCredential });
  google.accounts.id.renderButton(document.getElementById("gsi-button"), {
    theme: "filled_black",
    size: "large",
    text: mode === "signup" ? "signup_with" : "signin_with",
    shape: "pill",
    width: 280,
  });
}
boot();
