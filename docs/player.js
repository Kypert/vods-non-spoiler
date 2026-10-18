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
function setupFullscreen(onChange) {
  const wrap = $("wrap");
  const btn = $("fs");
  const native = wrap.requestFullscreen || wrap.webkitRequestFullscreen;
  const isOn = () => document.body.classList.contains("pseudo-fs") || !!(document.fullscreenElement || document.webkitFullscreenElement);
  const sync = () => {
    btn.textContent = isOn() ? "Exit full screen" : "Full screen";
    btn.setAttribute("aria-pressed", String(isOn()));
    onChange();
  };
  const exit = async () => {
    document.body.classList.remove("pseudo-fs");
    if (document.fullscreenElement) await document.exitFullscreen().catch(() => {});
    else if (document.webkitFullscreenElement) document.webkitExitFullscreen();
    sync();
  };

  btn.addEventListener("click", async () => {
    if (isOn()) {
      await exit();
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
  // Native full screen exits on Escape or the Android back gesture by itself, the CSS one does not
  document.addEventListener("keydown", (e) => {
    if (e.key === "Escape" && document.body.classList.contains("pseudo-fs")) exit();
  });
  document.addEventListener("fullscreenchange", sync);
  document.addEventListener("webkitfullscreenchange", sync);
  return { isOn, exit };
}

// In full screen, the controls float over the video and hide after a few seconds of play.
// A tap on the video shows them again. When paused, they stay visible.
const HIDE_AFTER_MS = 3000;

function setup(videoId) {
  const wrap = $("wrap");
  let playing = false;
  let hideTimer;
  const controlsHidden = () => wrap.classList.contains("controls-hidden");
  const hideControls = () => {
    clearTimeout(hideTimer);
    if (fs.isOn() && playing) wrap.classList.add("controls-hidden");
  };
  const showControls = () => {
    clearTimeout(hideTimer);
    wrap.classList.remove("controls-hidden");
    if (fs.isOn() && playing) hideTimer = setTimeout(hideControls, HIDE_AFTER_MS);
  };
  const fs = setupFullscreen(showControls);

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
          playing = e.data === YT.PlayerState.PLAYING;
          if (playing) started = true;
          cover.classList.toggle("clear", playing);
          cover.classList.remove("passthrough");
          cover.textContent = e.data === YT.PlayerState.PAUSED ? "Paused" : "Tap to play";
          $("play").textContent = playing ? "Pause" : "Play";
          if (playing && player.getPlaybackRate() !== Number(speed.value)) applySpeed();
          showControls();
          if (e.data === YT.PlayerState.ENDED) {
            clearInterval(timer);
            player.destroy();
            fs.exit();
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

  // In full screen while playing, a tap on the video shows or hides the controls instead of
  // pausing, so you do not pause by mistake. Pause with the Play/Pause button.
  cover.addEventListener("click", () => {
    if (fs.isOn() && playing) {
      if (controlsHidden()) showControls();
      else hideControls();
      return;
    }
    toggle();
  });
  const controls = $("controls");
  controls.addEventListener("pointerdown", showControls);
  controls.addEventListener("focusin", showControls);
  $("play").addEventListener("click", toggle);
  document.querySelectorAll("[data-seek]").forEach((b) => b.addEventListener("click", () => seek(Number(b.dataset.seek))));
  speed.addEventListener("change", () => {
    localStorage.setItem("speed", speed.value);
    if (player) applySpeed();
  });
  document.addEventListener("keydown", (e) => {
    showControls();
    if (e.target instanceof HTMLSelectElement) return;
    if (e.key === " " || e.key === "k") { e.preventDefault(); toggle(); }
    else if (e.key === "ArrowLeft") seek(e.shiftKey ? -60 : -10);
    else if (e.key === "ArrowRight") seek(e.shiftKey ? 60 : 10);
  });
}

main().catch((err) => showOnly(`Could not load data: ${err.message}`));
