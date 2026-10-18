// Plays one game without spoilers: no duration, no progress bar, no end screen.
// Speed is set via the IFrame Player API, because no URL parameter exists for it.
import { hashParams, loadJSON, playerURL, validSlug, watchedKey } from "./common.js";

// A hash change ("Next game") does not load a new page, so reload to start clean
window.addEventListener("hashchange", () => location.reload());

const params = hashParams();
const slug = params.get("t");
const matchId = params.get("m");
const gameNo = Number(params.get("g"));

const $ = (id) => document.getElementById(id);
const msg = $("msg");
const cover = $("cover");
const speed = $("speed");

speed.value = localStorage.getItem("speed") || "1.5";

function fmt(sec) {
  sec = Math.floor(sec);
  const h = Math.floor(sec / 3600);
  const m = Math.floor((sec % 3600) / 60);
  const s = String(sec % 60).padStart(2, "0");
  return h ? `${h}:${String(m).padStart(2, "0")}:${s}` : `${m}:${s}`;
}

function findMatch(data) {
  for (const st of data.stages) for (const d of st.days) for (const m of d.matches) if (m.id === matchId) return m;
  return null;
}

function showOnly(text) {
  $("stage").hidden = true;
  $("controls").hidden = true;
  msg.textContent = text;
}

async function main() {
  if (!validSlug(slug)) {
    showOnly("Unknown tournament.");
    return;
  }
  $("back").href = `index.html#t=${slug}`;
  const match = findMatch(await loadJSON(`data/${slug}.json`));
  if (!match || !match.ready || !(Number.isInteger(gameNo) && gameNo >= 1 && gameNo <= match.slots)) {
    showOnly("Unknown game.");
    return;
  }
  $("label").textContent = `Game ${gameNo}`;
  document.title = `Game ${gameNo}`;
  localStorage.setItem(watchedKey(matchId, gameNo), "1");

  // The next link looks the same for real and dummy games
  if (gameNo < match.slots) {
    const next = $("next");
    next.href = playerURL(slug, matchId, gameNo + 1);
    next.hidden = false;
  }

  const videoId = match.games[gameNo - 1];
  if (!videoId) {
    // Dummy slot: you only get here after the series is already decided
    showOnly("The series is over. No more games.");
    return;
  }
  setup(videoId);
}

// Real full screen where the browser supports it on elements (desktop, Android). iPhone
// Safari supports it only on <video>, and the video is inside YouTube's iframe, so there we
// fill the window with CSS. Turn the phone to landscape to get a big picture.
function setupFullscreen() {
  const wrap = $("wrap");
  const btn = $("fs");
  const native = wrap.requestFullscreen || wrap.webkitRequestFullscreen;
  const isOn = () => document.body.classList.contains("pseudo-fs") || !!(document.fullscreenElement || document.webkitFullscreenElement);
  const sync = () => {
    btn.textContent = isOn() ? "Exit full screen" : "Full screen";
    btn.setAttribute("aria-pressed", String(isOn()));
  };

  btn.addEventListener("click", async () => {
    if (isOn()) {
      document.body.classList.remove("pseudo-fs");
      if (document.fullscreenElement) await document.exitFullscreen();
      else if (document.webkitFullscreenElement) document.webkitExitFullscreen();
      sync();
      return;
    }
    if (native) {
      try {
        await native.call(wrap);
        // Android Chrome can lock the orientation in full screen. Other browsers reject it.
        await screen.orientation?.lock?.("landscape").catch(() => {});
        sync();
        return;
      } catch {
        // Fall through to the CSS full screen
      }
    }
    document.body.classList.add("pseudo-fs");
    sync();
  });
  document.addEventListener("fullscreenchange", sync);
  document.addEventListener("webkitfullscreenchange", sync);
}

function setup(videoId) {
  setupFullscreen();
  const tag = document.createElement("script");
  tag.src = "https://www.youtube.com/iframe_api";
  document.head.append(tag);

  let player;
  let timer;
  let started = false;
  const applySpeed = () => player.setPlaybackRate(Number(speed.value));

  window.onYouTubeIframeAPIReady = () => {
    player = new YT.Player("yt", {
      host: "https://www.youtube-nocookie.com",
      videoId,
      playerVars: { controls: 0, rel: 0, disablekb: 1, fs: 0, iv_load_policy: 3, playsinline: 1 },
      events: {
        onReady: applySpeed,
        onStateChange: (e) => {
          const playing = e.data === YT.PlayerState.PLAYING;
          if (playing) started = true;
          cover.classList.toggle("clear", playing);
          cover.classList.remove("passthrough");
          cover.textContent = e.data === YT.PlayerState.PAUSED ? "Paused" : "Tap to play";
          $("play").textContent = playing ? "Pause" : "Play";
          if (playing && player.getPlaybackRate() !== Number(speed.value)) applySpeed();
          if (e.data === YT.PlayerState.ENDED) {
            clearInterval(timer);
            player.destroy();
            document.body.classList.remove("pseudo-fs");
            if (document.fullscreenElement) document.exitFullscreen().catch(() => {});
            $("stage").hidden = true;
            $("controls").hidden = true;
            msg.textContent = "Game finished.";
          }
        },
      },
    });
    timer = setInterval(() => {
      if (player.getCurrentTime) $("elapsed").textContent = fmt(player.getCurrentTime());
    }, 500);
  };

  const toggle = () => {
    if (!player?.getPlayerState) return;
    if (player.getPlayerState() === YT.PlayerState.PLAYING) {
      player.pauseVideo();
      return;
    }
    player.playVideo();
    // Some mobile browsers block a scripted first play from outside the iframe. Then let
    // the next tap go to YouTube's own start button. That screen can show the video title.
    if (!started) {
      setTimeout(() => {
        if (started) return;
        cover.classList.add("passthrough");
        msg.textContent = "The browser blocked the start. Tap the video once more (YouTube's start screen can show the title).";
      }, 2000);
    }
  };
  const seek = (d) => player?.seekTo && player.seekTo(Math.max(0, player.getCurrentTime() + d), true);

  cover.addEventListener("click", toggle);
  $("play").addEventListener("click", toggle);
  document.querySelectorAll("[data-seek]").forEach((b) => b.addEventListener("click", () => seek(Number(b.dataset.seek))));
  speed.addEventListener("change", () => {
    localStorage.setItem("speed", speed.value);
    if (player) applySpeed();
  });
  document.addEventListener("keydown", (e) => {
    if (e.target instanceof HTMLSelectElement) return;
    if (e.key === " " || e.key === "k") { e.preventDefault(); toggle(); }
    else if (e.key === "ArrowLeft") seek(e.shiftKey ? -60 : -10);
    else if (e.key === "ArrowRight") seek(e.shiftKey ? 60 : 10);
  });
}

main().catch((err) => showOnly(`Could not load data: ${err.message}`));
