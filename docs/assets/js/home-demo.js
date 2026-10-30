// Scroll-driven homepage demo (content/_index.md). Scroll progress through
// .sc-demo-track maps to three scenes; CSS reads --s1..--s3 (0..1 each).
// While the stage is pinned, scrolling pages between the REST stops (the
// progress-bar dots): each wheel/swipe/key gesture is one step in its
// direction. Gestures made while a step is still animating are queued (as a
// signed count) and replayed when it lands; once the queue runs past the
// first/last stop, the page scrolls out of the demo and behaves normally.
(() => {
  const root = document.querySelector("[data-sc-demo]");
  if (!root) return;
  const track = root.querySelector(".sc-demo-track");
  const stage = root.querySelector(".sc-demo-stage");
  const steps = [...root.querySelectorAll(".sc-step")];
  const term = root.querySelector(".sc-term-body");
  const nav = document.querySelector(".hextra-nav-container");
  const lines = [...root.querySelectorAll(".sc-l")].map((el) => {
    const [a, b] = el.dataset.at.split(",").map(Number);
    return { el, a, b, text: el.dataset.type };
  });
  const packets = [...root.querySelectorAll(".sc-packets i")];
  const reduce = window.matchMedia("(prefers-reduced-motion: reduce)");
  const clamp = (v) => Math.min(1, Math.max(0, v));
  const SCENES = 3;
  // each scene rests at 92% of its span (matches --fN in custom.css); the
  // last one rests at the very end so leaving the demo is seamless
  const REST = [0, 0.92 / SCENES, 1.92 / SCENES, 1];
  const EPS = 0.002;
  const DURATION = 750;
  const GAP = 150; // ms without wheel input that separates two gestures
  const EXIT = 0.7; // viewport heights scrolled when the queue leaves the demo

  function render(p) {
    for (let i = 0; i < SCENES; i++) {
      root.style.setProperty(`--s${i + 1}`, clamp(p * SCENES - i).toFixed(4));
    }
    root.dataset.scene = String(Math.min(SCENES, Math.floor(p * SCENES) + 1));
    steps.forEach((el, i) => el.classList.toggle("is-done", p >= REST[i + 1] - EPS));
    for (const l of lines) {
      const t = l.b > l.a ? clamp((p - l.a) / (l.b - l.a)) : p >= l.a ? 1 : 0;
      l.el.textContent = l.text.slice(0, Math.round(l.text.length * t));
      l.el.classList.toggle("is-typing", t > 0 && t < 1);
    }
    let last = 0;
    lines.forEach((l, i) => (l.el.textContent ? (last = i) : null));
    term.scrollTop = Math.max(0, lines[last].el.offsetTop + lines[last].el.offsetHeight - term.clientHeight + 10);
    // packets stream from the source to the registry while scene 1 fills
    const s1 = clamp(p * SCENES);
    const flowing = s1 > 0.08 && s1 < 0.7;
    packets.forEach((el, i) => {
      const x = (s1 * 6 + i / packets.length) % 1;
      el.style.setProperty("--x", x.toFixed(4));
      el.style.opacity = flowing ? String(Math.sin(Math.PI * x)) : "0";
    });
  }

  // pin the stage vertically centered in the area below the navbar
  function center() {
    const navH = nav ? nav.offsetHeight : 56;
    const free = window.innerHeight - navH - stage.offsetHeight;
    stage.style.top = `${navH + Math.max(0, free / 2)}px`;
  }
  // geometry: progress == (scrollY - start) / span
  function geom() {
    const stickyTop = parseFloat(stage.style.top) || 0;
    const span = track.offsetHeight - stage.offsetHeight;
    const start = track.getBoundingClientRect().top + window.scrollY - stickyTop;
    return { span, start, raw: span > 0 ? (window.scrollY - start) / span : 1 };
  }
  const live = () => !reduce.matches;
  const pinned = (r) => r > -EPS && r < 1 + EPS;
  function nextStop(r, dir) {
    return dir > 0 ? REST.find((s) => s > r + EPS) : [...REST].reverse().find((s) => s < r - EPS);
  }

  let queued = false;
  function update() {
    queued = false;
    if (!live()) {
      root.classList.remove("sc-demo-live");
      stage.style.top = "";
      render(1);
      steps.forEach((el) => el.classList.remove("is-done"));
      delete root.dataset.scene;
      return;
    }
    root.classList.add("sc-demo-live");
    center();
    render(clamp(geom().raw));
  }
  function schedule() {
    if (!queued) {
      queued = true;
      requestAnimationFrame(update);
    }
  }

  // eased scroll to an absolute scrollY; queued steps run when it lands
  let animating = false;
  let pending = 0; // signed number of queued steps (+ down, - up)
  function scrollToY(to) {
    const from = window.scrollY;
    const t0 = performance.now();
    animating = true;
    const ease = (t) => (t < 0.5 ? 4 * t * t * t : 1 - (-2 * t + 2) ** 3 / 2);
    (function frame(now) {
      const t = Math.min(1, (now - t0) / DURATION);
      window.scrollTo({ top: from + (to - from) * ease(t), behavior: "instant" });
      if (t < 1) return requestAnimationFrame(frame);
      animating = false;
      drain();
    })(t0);
  }
  function go(p) {
    const { span, start } = geom();
    scrollToY(start + p * span);
  }
  function drain() {
    if (!pending) return;
    const dir = Math.sign(pending);
    pending -= dir;
    const { span, start, raw } = geom();
    const target = nextStop(raw, dir);
    if (target !== undefined) return go(target);
    // queue ran past the boundary: leave the demo, one EXIT per leftover step
    const n = 1 + Math.abs(pending);
    pending = 0;
    const edge = dir > 0 ? Math.max(window.scrollY, start + span) : Math.min(window.scrollY, start);
    scrollToY(edge + dir * n * EXIT * window.innerHeight);
  }
  // one gesture = one step; returns true when the demo consumed it
  function request(dir) {
    if (!live()) return false;
    if (animating) {
      pending += dir;
      return true;
    }
    const { raw } = geom();
    if (!pinned(raw)) return false;
    const target = nextStop(raw, dir);
    if (target === undefined) return false;
    go(target);
    return true;
  }

  // wheel events arrive in bursts (trackpad momentum, fast mouse spins); a
  // new gesture starts after a pause, on a direction change, or when the
  // delta jumps up again while the previous burst was decaying
  let last = { t: -Infinity, abs: 0, peak: 0, dir: 0 };
  let consumed = false; // whether the demo owns the current gesture
  window.addEventListener(
    "wheel",
    (e) => {
      if (!live() || e.ctrlKey || Math.abs(e.deltaY) < Math.abs(e.deltaX)) return;
      const now = performance.now();
      const abs = Math.abs(e.deltaY);
      const dir = Math.sign(e.deltaY);
      const fresh =
        now - last.t > GAP || dir !== last.dir || (abs > last.abs * 1.8 + 4 && last.abs < last.peak * 0.6);
      last = { t: now, abs, dir, peak: fresh ? abs : Math.max(last.peak, abs) };
      if (fresh || (!consumed && pinned(geom().raw))) {
        consumed = request(dir);
      } else if (!animating && !pinned(geom().raw)) {
        consumed = false;
      }
      if (consumed) e.preventDefault();
    },
    { passive: false },
  );

  let touchY = null;
  window.addEventListener("touchstart", (e) => (touchY = e.touches[0].clientY), { passive: true });
  window.addEventListener(
    "touchmove",
    (e) => {
      if (touchY === null) return animating ? e.preventDefault() : undefined;
      const dy = touchY - e.touches[0].clientY;
      if (Math.abs(dy) < 12) return animating ? e.preventDefault() : undefined;
      if (request(Math.sign(dy))) {
        e.preventDefault();
        touchY = null;
      }
    },
    { passive: false },
  );
  window.addEventListener("touchend", () => (touchY = null), { passive: true });

  const KEYS = { ArrowDown: 1, PageDown: 1, " ": 1, ArrowUp: -1, PageUp: -1 };
  window.addEventListener("keydown", (e) => {
    const t = e.target;
    if (!(e.key in KEYS) || e.altKey || e.ctrlKey || e.metaKey) return;
    if (t.isContentEditable || /^(INPUT|TEXTAREA|SELECT|BUTTON)$/.test(t.tagName)) return;
    const dir = e.key === " " && e.shiftKey ? -1 : KEYS[e.key];
    if (request(dir)) e.preventDefault();
  });

  // scrollbar drags and anchor jumps: settle on the nearest stop
  function settle() {
    if (!live() || animating || pending) return;
    const { span, raw } = geom();
    if (span <= 0 || raw <= 0 || raw >= 1) return;
    const target = REST.reduce((best, r) => (Math.abs(r - raw) < Math.abs(best - raw) ? r : best));
    if (Math.abs(target - raw) * span > 2) go(target);
  }
  let idle;
  window.addEventListener(
    "scroll",
    () => {
      schedule();
      if (!("onscrollend" in window)) {
        clearTimeout(idle);
        idle = setTimeout(settle, 150);
      }
    },
    { passive: true },
  );
  window.addEventListener("scrollend", settle);

  steps.forEach((el, i) =>
    el.addEventListener("click", () => {
      if (!live()) return;
      pending = 0;
      go(REST[i + 1]);
    }),
  );
  window.addEventListener("resize", schedule);
  reduce.addEventListener("change", schedule);
  update();
})();
