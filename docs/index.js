// Two views: the tournament list (no hash) and one tournament (#t=<slug>).
// Team names are hidden per day until revealed, because later pairings tell the results of
// earlier rounds.
import { TZ, el, hashParams, loadJSON, playerURL, validSlug, watchedKey } from "./common.js";

const app = document.getElementById("app");
const title = document.getElementById("title");
const hint = document.getElementById("hint");

const revealKey = (slug, stage, date) => `reveal:${slug}:${stage}:${date}`;
const tabKey = (slug) => `tab:${slug}`;

const timeFmt = new Intl.DateTimeFormat([], { timeZone: TZ, hour: "2-digit", minute: "2-digit" });
const dayFmt = new Intl.DateTimeFormat([], { timeZone: TZ, weekday: "short", day: "numeric", month: "short" });

function dayLabel(date) {
  // Noon UTC is the same calendar day in Europe/Stockholm
  return dayFmt.format(new Date(`${date}T12:00:00Z`));
}

async function renderList() {
  title.textContent = "Spoiler-free VODs";
  hint.hidden = true;
  document.title = "Spoiler-free VODs";
  const index = await loadJSON("data/index.json");
  const ul = el("ul", { class: "tournaments" });
  for (const t of index) {
    if (!validSlug(t.slug)) continue;
    ul.append(el("li", {}, el("a", { href: `#t=${t.slug}` }, t.name), el("span", { class: "dates" }, ` ${t.startDate} – ${t.endDate}`)));
  }
  app.replaceChildren(ul);
}

function renderDay(slug, stage, day) {
  const key = revealKey(slug, stage.name, day.date);
  const revealed = localStorage.getItem(key) === "1";
  const btn = el("button", { type: "button", class: "reveal", "aria-pressed": String(revealed) }, revealed ? "Hide teams" : "Show teams");
  btn.addEventListener("click", () => {
    if (revealed) localStorage.removeItem(key);
    else localStorage.setItem(key, "1");
    btn.closest("section").replaceWith(renderDay(slug, stage, day));
  });

  const list = el("ul", { class: "matches" });
  day.matches.forEach((m, i) => {
    const name = revealed ? `${m.teams[0]} vs ${m.teams[1]}` : `Series ${i + 1}`;
    const games = el("div", { class: "games" });
    for (let g = 1; g <= m.slots; g++) {
      if (!m.ready) {
        games.append(el("span", { class: "game pending", "aria-label": `Game ${g}, not ready yet` }, `G${g}`));
        continue;
      }
      const watched = localStorage.getItem(watchedKey(m.id, g)) === "1";
      games.append(el("a", {
        class: watched ? "game watched" : "game",
        href: playerURL(slug, m.id, g),
        "aria-label": `Game ${g}${watched ? ", opened before" : ""}`,
      }, `G${g}`));
    }
    list.append(el("li", { class: "match" },
      el("span", { class: "start" }, timeFmt.format(new Date(m.start))),
      el("span", { class: "teams" }, name),
      games));
  });
  return el("section", { class: "day" }, el("div", { class: "day-head" }, el("h3", {}, dayLabel(day.date)), btn), list);
}

async function renderTournament(slug) {
  const data = await loadJSON(`data/${slug}.json`);
  title.textContent = data.name;
  document.title = data.name;
  hint.hidden = false;

  const stages = data.stages;
  if (stages.length === 0) {
    app.replaceChildren(el("p", {}, "No series yet."));
    return;
  }
  const saved = Number(localStorage.getItem(tabKey(slug)));
  let current = Number.isInteger(saved) && saved >= 0 && saved < stages.length ? saved : 0;

  const tabs = el("div", { class: "tabs", role: "tablist", "aria-label": "Stages" });
  const panel = el("div", { class: "panel", role: "tabpanel", id: "panel" });

  const show = (i) => {
    current = i;
    localStorage.setItem(tabKey(slug), String(i));
    tabs.querySelectorAll("[role=tab]").forEach((b, j) => {
      b.setAttribute("aria-selected", String(j === i));
      b.tabIndex = j === i ? 0 : -1;
    });
    panel.replaceChildren(...stages[i].days.map((d) => renderDay(slug, stages[i], d)));
  };

  stages.forEach((s, i) => {
    const b = el("button", { type: "button", role: "tab", "aria-controls": "panel" }, s.name);
    b.addEventListener("click", () => show(i));
    b.addEventListener("keydown", (e) => {
      const d = e.key === "ArrowRight" ? 1 : e.key === "ArrowLeft" ? -1 : 0;
      if (!d) return;
      const n = (current + d + stages.length) % stages.length;
      show(n);
      tabs.children[n].focus();
    });
    tabs.append(b);
  });

  app.replaceChildren(el("nav", {}, el("a", { href: "#" }, "← All tournaments")), tabs, panel);
  show(current);
}

async function route() {
  try {
    const slug = hashParams().get("t");
    if (slug && validSlug(slug)) await renderTournament(slug);
    else await renderList();
  } catch (err) {
    app.textContent = `Could not load data: ${err.message}`;
  }
}

window.addEventListener("hashchange", route);
route();
