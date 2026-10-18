// Shared helpers for the list and the player page

// Day grouping in the data uses this time zone, so the page shows times in it too
export const TZ = "Europe/Stockholm";

export function el(tag, attrs = {}, ...children) {
  const e = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs)) {
    if (k === "class") e.className = v;
    else if (v !== undefined && v !== null) e.setAttribute(k, v);
  }
  e.append(...children);
  return e;
}

export function hashParams() {
  return new URLSearchParams(location.hash.slice(1));
}

// Slugs come from the URL, so allow only safe characters before we use them in a path
export function validSlug(slug) {
  return typeof slug === "string" && /^[a-z0-9_-]+$/.test(slug);
}

export async function loadJSON(path) {
  const resp = await fetch(path, { cache: "no-store" });
  if (!resp.ok) throw new Error(`${path}: ${resp.status} ${resp.statusText}`);
  return resp.json();
}

export const playerURL = (slug, matchId, game) => `player.html#t=${slug}&m=${matchId}&g=${game}`;
export const watchedKey = (matchId, game) => `watched:${matchId}:${game}`;
