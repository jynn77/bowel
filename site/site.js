// bowel's website: screenshots that follow the page's language and the color
// scheme, the skin gallery, downloads, copy buttons, and a little entrance motion.
(function () {
  var root = document.documentElement;
  var dark = window.matchMedia("(prefers-color-scheme: dark)");
  // Each language is its own page (scripts/build-site.py), marked on <html>.
  var lang = root.classList.contains("en") ? "en" : "zh";
  // The screenshots sit beside this script, whichever directory the page is in.
  var here = document.currentScript.src;
  var shot = function (name) { return new URL("shots/" + name + ".webp", here).href; };

  // Every <img data-shot="name"> shows shots/name-<lang>[-dark].webp.
  // data-dark="1" keeps it dark whatever the page's scheme.
  function shots() {
    document.querySelectorAll("img[data-shot]").forEach(function (img) {
      var d = img.dataset.dark === "1" || dark.matches;
      var src = shot(img.dataset.shot + "-" + lang + (d ? "-dark" : ""));
      if (img.src !== src) img.src = src;
    });
    skin(current, true);
  }

  dark.addEventListener("change", shots);

  // Skins.
  var current = "paper";
  var skinImg = document.getElementById("skin-img");
  var tabs = document.querySelectorAll(".skin-tabs button");
  function skin(id, quiet) {
    current = id;
    tabs.forEach(function (b) { b.setAttribute("aria-selected", String(b.dataset.skin === id)); });
    document.querySelectorAll("[data-note]").forEach(function (n) { n.hidden = n.dataset.note !== id; });
    var src = shot("skin-" + id + "-" + lang);
    if (skinImg.src === src) return;
    if (quiet) { skinImg.src = src; return; }
    var next = new Image();
    next.src = src;
    skinImg.classList.add("fading");
    next.decode().catch(function () {}).then(function () {
      skinImg.src = src;
      skinImg.classList.remove("fading");
    });
  }
  tabs.forEach(function (b) {
    b.addEventListener("click", function () { skin(b.dataset.skin); });
    // Warm the cache so switching is instant.
    b.addEventListener("pointerenter", function () { new Image().src = shot("skin-" + b.dataset.skin + "-" + lang); }, { once: true });
  });

  // Copy buttons.
  document.addEventListener("click", function (e) {
    var b = e.target.closest(".copy-btn");
    if (!b) return;
    navigator.clipboard.writeText(b.dataset.copy).then(function () {
      b.classList.add("done");
      setTimeout(function () { b.classList.remove("done"); }, 1400);
    });
  });

  // Get bowel: the hero's block is copied into #download, and both show
  // one platform, the visitor's to begin with.
  var get = document.querySelector(".get");
  document.querySelectorAll(".get-slot").forEach(function (slot) {
    slot.replaceChildren(get.cloneNode(true));
  });
  function platform(os) {
    document.querySelectorAll(".get-tabs button").forEach(function (b) { b.setAttribute("aria-selected", String(b.dataset.os === os)); });
    document.querySelectorAll(".get-panel").forEach(function (p) { p.hidden = p.dataset.os !== os; });
  }
  document.addEventListener("click", function (e) {
    var b = e.target.closest(".get-tabs button");
    if (b) platform(b.dataset.os);
  });
  var ua = navigator.userAgent;
  platform(/Android/i.test(ua) ? "android" : /Windows/i.test(ua) ? "windows" : /Linux|X11|CrOS/i.test(ua) ? "linux" : "macos");

  // File names carry the version, so the links start at the release page
  // and move to the files once GitHub says what the latest release holds.
  var assets = {
    "dmg": /-arm64\.dmg$/, "apk": /-android-arm64\.apk$/,
    "exe-amd64": /^bowel-setup-.*-amd64\.exe$/, "exe-arm64": /^bowel-setup-.*-arm64\.exe$/,
    "deb-amd64": /_amd64\.deb$/, "deb-arm64": /_arm64\.deb$/,
    "rpm-amd64": /\.x86_64\.rpm$/, "rpm-arm64": /\.aarch64\.rpm$/,
    "tgz-amd64": /-linux-amd64\.tar\.gz$/, "tgz-arm64": /-linux-arm64\.tar\.gz$/
  };
  var base = "https://github.com/21stware/bowel/releases/download/";
  function release(rel) {
    if (!rel || !Array.isArray(rel.files)) return;
    document.querySelectorAll("a[data-asset]").forEach(function (a) {
      var re = assets[a.dataset.asset];
      var f = rel.files.find(function (f) { return re.test(f.name) && String(f.url).indexOf(base) === 0; });
      if (f) a.href = f.url;
    });
    if (/^v[\d.]+$/.test(rel.tag)) document.querySelectorAll(".get-ver").forEach(function (v) { v.textContent = rel.tag; v.hidden = false; });
  }
  var cached = null;
  try { cached = JSON.parse(sessionStorage.getItem("bowel-release")); } catch (e) {}
  if (cached) release(cached);
  else if (window.fetch) {
    fetch("https://api.github.com/repos/21stware/bowel/releases/latest", { headers: { Accept: "application/vnd.github+json" } })
      .then(function (r) { return r.ok ? r.json() : Promise.reject(); })
      .then(function (j) {
        var rel = { tag: j.tag_name, files: j.assets.map(function (a) { return { name: a.name, url: a.browser_download_url }; }) };
        release(rel);
        try { sessionStorage.setItem("bowel-release", JSON.stringify(rel)); } catch (e) {}
      })
      .catch(function () {});
  }

  // The nav gets a hairline once the page scrolls.
  var nav = document.querySelector(".nav");
  var onScroll = function () { nav.classList.toggle("scrolled", window.scrollY > 8); };
  window.addEventListener("scroll", onScroll, { passive: true });
  onScroll();

  // Sections rise in as they come into view.
  if ("IntersectionObserver" in window) {
    var io = new IntersectionObserver(function (es) {
      es.forEach(function (e) { if (e.isIntersecting) { e.target.classList.add("in"); io.unobserve(e.target); } });
    }, { rootMargin: "0px 0px -8% 0px" });
    document.querySelectorAll(".feature, .agents > *, .cell, .skins > *, .download > *").forEach(function (el) {
      el.classList.add("reveal");
      io.observe(el);
    });
  }

  shots();
})();
