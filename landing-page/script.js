/* RemoraSFTP landing page interactions. */
(function () {
  'use strict';

  var GH_URL = 'https://github.com/flessan/RemoraSFTP';
  var RELEASES_URL = GH_URL + '/releases';
  var THREE_CDN = 'https://cdn.jsdelivr.net/npm/three@0.160.0/build/three.module.js';
  var reducedMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
  var $ = function (s, c) { return (c || document).querySelector(s); };
  var $$ = function (s, c) { return Array.prototype.slice.call((c || document).querySelectorAll(s)); };

  /* Keep all repository/release links pointed at the canonical repository. */
  $$('a[href*="github.com/34labs/RemoraSFTP-dev"]').forEach(function (a) {
    var old = a.getAttribute('href');
    a.setAttribute('href', old.indexOf('/releases') !== -1 ? RELEASES_URL : GH_URL);
  });

  /* Theme */
  var root = document.documentElement;
  var metaTheme = $('#meta-theme');
  function applyTheme(theme) {
    root.setAttribute('data-theme', theme);
    if (metaTheme) metaTheme.setAttribute('content', theme === 'dark' ? '#0a1219' : '#f5f8f9');
    try { localStorage.setItem('remora-theme', theme); } catch (e) {}
  }
  try {
    var saved = localStorage.getItem('remora-theme');
    if (saved === 'dark' || saved === 'light') applyTheme(saved);
  } catch (e) {}
  var themeBtn = $('#theme-toggle');
  if (themeBtn) themeBtn.addEventListener('click', function () {
    applyTheme(root.getAttribute('data-theme') === 'dark' ? 'light' : 'dark');
  });

  /* Scroll UI */
  var nav = $('.nav'), progress = $('#progress-bar'), topBtn = $('#to-top');
  function onScroll() {
    if (nav) nav.classList.toggle('scrolled', window.scrollY > 8);
    var max = document.documentElement.scrollHeight - document.documentElement.clientHeight;
    if (progress) progress.style.transform = 'scaleX(' + (max ? Math.min(1, window.scrollY / max) : 0) + ')';
    if (topBtn) topBtn.classList.toggle('show', window.scrollY > 600);
  }
  window.addEventListener('scroll', onScroll, { passive: true });
  onScroll();
  if (topBtn) topBtn.addEventListener('click', function () {
    window.scrollTo({ top: 0, behavior: reducedMotion ? 'auto' : 'smooth' });
  });

  /* Mobile navigation */
  var burger = $('#nav-burger'), navLinks = $('#nav-links');
  function closeNav() {
    if (!navLinks || !burger) return;
    navLinks.classList.remove('open');
    burger.setAttribute('aria-expanded', 'false');
  }
  if (burger && navLinks) {
    burger.addEventListener('click', function () {
      var open = navLinks.classList.toggle('open');
      burger.setAttribute('aria-expanded', open ? 'true' : 'false');
    });
    navLinks.addEventListener('click', function (e) {
      if (e.target.closest('a')) closeNav();
    });
  }

  /* Reveal-on-scroll */
  var reveals = $$('.reveal');
  if (reducedMotion || !('IntersectionObserver' in window)) {
    reveals.forEach(function (el) { el.classList.add('visible'); });
  } else {
    var rio = new IntersectionObserver(function (entries) {
      entries.forEach(function (entry) {
        if (entry.isIntersecting) {
          entry.target.classList.add('visible');
          rio.unobserve(entry.target);
        }
      });
    }, { threshold: 0.12, rootMargin: '0px 0px -40px 0px' });
    reveals.forEach(function (el) { rio.observe(el); });
  }

  /* Section spy */
  if ('IntersectionObserver' in window) {
    var spyMap = {};
    $$('.nav-links a[href^="#"]').forEach(function (a) { spyMap[a.getAttribute('href').slice(1)] = a; });
    var sio = new IntersectionObserver(function (entries) {
      entries.forEach(function (entry) {
        if (!entry.isIntersecting || !spyMap[entry.target.id]) return;
        $$('.nav-links a.active').forEach(function (a) { a.classList.remove('active'); });
        spyMap[entry.target.id].classList.add('active');
      });
    }, { rootMargin: '-40% 0px -55% 0px' });
    Object.keys(spyMap).forEach(function (id) {
      var el = document.getElementById(id);
      if (el) sio.observe(el);
    });
  }

  /* Counters */
  $$('.stat-chips').forEach(function (group) {
    var run = function () {
      $$('[data-count]', group).forEach(function (el) {
        var target = parseInt(el.getAttribute('data-count'), 10) || 0;
        var pre = el.getAttribute('data-prefix') || '';
        var suf = el.getAttribute('data-suffix') || '';
        if (reducedMotion || target === 0) { el.textContent = pre + target + suf; return; }
        var start = null;
        function frame(ts) {
          if (!start) start = ts;
          var p = Math.min(1, (ts - start) / 900);
          el.textContent = pre + Math.round((1 - Math.pow(1 - p, 3)) * target) + suf;
          if (p < 1) requestAnimationFrame(frame);
        }
        requestAnimationFrame(frame);
      });
    };
    if ('IntersectionObserver' in window) {
      var io = new IntersectionObserver(function (entries) {
        if (entries[0].isIntersecting) { run(); io.disconnect(); }
      }, { threshold: 0.4 });
      io.observe(group);
    } else run();
  });

  /* FAQ */
  $$('.faq-item').forEach(function (item) {
    var q = $('.faq-q', item);
    if (!q) return;
    q.addEventListener('click', function () {
      var open = item.classList.toggle('open');
      q.setAttribute('aria-expanded', open ? 'true' : 'false');
    });
  });

  /* Clipboard + toast */
  var toastEl = $('#toast'), toastTimer;
  function toast(msg) {
    if (!toastEl) return;
    toastEl.textContent = msg;
    toastEl.classList.add('show');
    clearTimeout(toastTimer);
    toastTimer = setTimeout(function () { toastEl.classList.remove('show'); }, 1800);
  }
  function copyText(text) {
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(text).then(function () { toast('Copied to clipboard'); }, function () { toast('Copy failed - select the text manually'); });
      return;
    }
    var ta = document.createElement('textarea');
    ta.value = text; ta.style.cssText = 'position:fixed;opacity:0';
    document.body.appendChild(ta); ta.select();
    try { document.execCommand('copy'); toast('Copied to clipboard'); } catch (e) { toast('Copy failed - select the text manually'); }
    document.body.removeChild(ta);
  }
  $$('.copy-btn').forEach(function (btn) {
    btn.addEventListener('click', function () {
      var pre = btn.closest('.code') && btn.closest('.code').querySelector('pre');
      if (pre) copyText(pre.innerText);
      var label = $('span', btn);
      if (label) {
        var old = label.textContent;
        label.textContent = 'Copied ✓';
        setTimeout(function () { label.textContent = old; }, 1400);
      }
    });
  });

  /* Make release/version information honest instead of hard-coded stale data. */
  function updateReleaseInfo() {
    var versionChip = $('.ver-chip');
    var versionText = $('#code-version pre');
    fetch('https://api.github.com/repos/flessan/RemoraSFTP/releases/latest', {
      headers: { Accept: 'application/vnd.github+json' }
    }).then(function (r) {
      if (!r.ok) throw new Error('release lookup failed');
      return r.json();
    }).then(function (release) {
      var tag = release.tag_name || '';
      if (!tag) return;
      if (versionChip) versionChip.textContent = tag;
      if (versionText) {
        versionText.textContent =
          '$ remorasftp version\\n' +
          'RemoraSFTP ' + tag.replace(/^v/, '') + '\\n' +
          '  release:  ' + tag + '\\n' +
          '  source:   ' + release.html_url;
      }
      $$('.dl-card').forEach(function (card) { card.setAttribute('href', RELEASES_URL + '/tag/' + encodeURIComponent(tag)); });
    }).catch(function () {
      if (versionChip) versionChip.textContent = 'Latest release';
      $$('.dl-card').forEach(function (card) { card.setAttribute('href', RELEASES_URL); });
    });
  }
  updateReleaseInfo();

  /* OS recommendation */
  (function () {
    var ua = navigator.userAgent || '';
    var os = /Windows/i.test(ua) ? 'windows' : (/Linux/i.test(ua) ? 'linux' : (/Macintosh|Mac OS X|iPhone|iPad/i.test(ua) ? 'macos' : ''));
    if (!os) return;
    $$('.dl-card[data-os]').forEach(function (card) {
      var kind = card.getAttribute('data-os');
      if ((os === 'windows' && kind === 'windows') ||
          (os === 'linux' && kind === 'linux') ||
          (os === 'macos' && kind === 'macos-arm')) {
        card.classList.add('recommended');
        var badge = $('.rec-badge', card);
        if (badge) badge.hidden = false;
      }
    });
  })();

  /* Command palette */
  var overlay = $('#palette-overlay'), input = $('#palette-input'), list = $('#palette-list'), paletteBtn = $('#palette-open');
  var actions = [
    ['Go to Overview', '#what'], ['Go to Features', '#features'], ['Go to Protocols', '#protocols'],
    ['Go to Terminal & CLI', '#tui'], ['Go to Security', '#security'], ['Go to Comparison', '#compare'],
    ['Go to FAQ', '#faq'], ['Go to Download', '#download']
  ];
  var filtered = [], selected = 0;
  function renderPalette(query) {
    if (!list) return;
    var q = (query || '').toLowerCase();
    filtered = actions.filter(function (a) { return !q || a[0].toLowerCase().indexOf(q) !== -1; });
    selected = 0; list.innerHTML = '';
    filtered.forEach(function (a, i) {
      var li = document.createElement('li');
      li.className = 'palette-item' + (i === 0 ? ' selected' : '');
      li.setAttribute('role', 'option');
      li.textContent = a[0];
      li.addEventListener('click', function () { closePalette(); var el = $(a[1]); if (el) el.scrollIntoView({ behavior: reducedMotion ? 'auto' : 'smooth' }); });
      list.appendChild(li);
    });
    if (!filtered.length) {
      var empty = document.createElement('li'); empty.className = 'palette-empty'; empty.textContent = 'No matching commands'; list.appendChild(empty);
    }
  }
  function openPalette() {
    if (!overlay) return;
    overlay.hidden = false;
    requestAnimationFrame(function () { overlay.classList.add('show'); });
    renderPalette('');
    if (input) { input.value = ''; setTimeout(function () { input.focus(); }, 0); }
    document.body.style.overflow = 'hidden';
  }
  function closePalette() {
    if (!overlay) return;
    overlay.classList.remove('show');
    document.body.style.overflow = '';
    setTimeout(function () { overlay.hidden = true; }, 150);
  }
  if (paletteBtn) paletteBtn.addEventListener('click', openPalette);
  if (overlay) overlay.addEventListener('click', function (e) { if (e.target === overlay) closePalette(); });
  if (input) {
    input.addEventListener('input', function () { renderPalette(input.value); });
    input.addEventListener('keydown', function (e) {
      if (e.key === 'Escape') { closePalette(); return; }
      if (!filtered.length) return;
      if (e.key === 'ArrowDown') { e.preventDefault(); selected = (selected + 1) % filtered.length; }
      if (e.key === 'ArrowUp') { e.preventDefault(); selected = (selected - 1 + filtered.length) % filtered.length; }
      if (e.key === 'Enter') {
        e.preventDefault();
        var a = filtered[selected], el = a && $(a[1]);
        closePalette();
        if (el) el.scrollIntoView({ behavior: reducedMotion ? 'auto' : 'smooth' });
      }
      $$('.palette-item', list).forEach(function (el, i) { el.classList.toggle('selected', i === selected); });
    });
  }
  document.addEventListener('keydown', function (e) {
    if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'k') {
      e.preventDefault(); if (overlay && overlay.hidden) openPalette(); else closePalette();
    }
  });

  /* Lightweight live terminal animation, if the section exists. */
  var term = $('#term-text');
  if (term) {
    var lines = [
      '$ remorasftp connect production',
      '✓ host key verified - saved to local trust store',
      '  connected · sftp · 10.40.0.12:/srv',
      '$ remorasftp put ./build.tar.gz /srv/backups/',
      '↑ build.tar.gz  [████████████████████] 100% · 3.1 MB/s'
    ];
    term.textContent = lines.join('\n');
  }

  /* Three.js hero. CDN failure and WebGL failure both fall back to the SVG. */
  var hero = $('.hero'), visual = $('#hero-visual'), canvas = $('#hero-canvas'), fallback = $('#hero-fallback');
  function fallbackHero() {
    if (canvas) canvas.style.display = 'none';
    if (fallback) fallback.hidden = false;
  }
  async function initHero() {
    if (!hero || !visual || !canvas || reducedMotion) { if (reducedMotion) fallbackHero(); return; }
    try {
      var c = document.createElement('canvas');
      if (!window.WebGLRenderingContext || !(c.getContext('webgl2') || c.getContext('webgl'))) throw new Error('WebGL unavailable');
      var THREE = await import(THREE_CDN);
      var renderer = new THREE.WebGLRenderer({ canvas: canvas, antialias: true, alpha: true, powerPreference: 'low-power' });
      var scene = new THREE.Scene();
      var camera = new THREE.PerspectiveCamera(42, 1, 0.1, 40);
      camera.position.z = 6.6;
      scene.add(new THREE.HemisphereLight(0xbfd8dd, 0x14212b, 1.05));
      var light = new THREE.DirectionalLight(0xffffff, 1.15); light.position.set(4, 6, 3); scene.add(light);
      var group = new THREE.Group(); scene.add(group);
      var folderMat = new THREE.MeshLambertMaterial({ color: 0x3d566b });
      var folder = new THREE.Group();
      var body = new THREE.Mesh(new THREE.BoxGeometry(1.7, 1.15, 0.22), folderMat);
      var tab = new THREE.Mesh(new THREE.BoxGeometry(0.62, 0.16, 0.22), folderMat); tab.position.set(-0.5, 0.66, 0);
      folder.add(body, tab); group.add(folder);
      var palette = [0x63b9bf, 0x8fa8bd, 0xd9c9a3], positions = [], files = [];
      var total = window.innerWidth < 768 ? 8 : 14;
      for (var i = 0; i < total; i++) {
        var angle = i / total * Math.PI * 2;
        var radius = i % 2 ? 3.35 : 2.25;
        var x = Math.cos(angle) * radius, y = Math.sin(i * 2.4) * 0.45, z = Math.sin(angle) * radius;
        var mesh = new THREE.Mesh(new THREE.BoxGeometry(0.55, 0.7, 0.06), new THREE.MeshLambertMaterial({ color: palette[i % palette.length] }));
        mesh.position.set(x, y, z); mesh.rotation.y = angle + Math.PI / 2; group.add(mesh);
        positions.push(x, y, z); files.push({ mesh: mesh, y: y, phase: i * 0.7 });
      }
      var geo = new THREE.BufferGeometry(); geo.setAttribute('position', new THREE.Float32BufferAttribute(positions, 3));
      var pts = new THREE.Points(geo, new THREE.PointsMaterial({ color: 0x8ed4d8, size: 0.045, transparent: true, opacity: 0.35 }));
      scene.add(pts);
      function resize() {
        var w = visual.clientWidth, h = visual.clientHeight; if (!w || !h) return;
        renderer.setPixelRatio(Math.min(window.devicePixelRatio || 1, 2));
        renderer.setSize(w, h, false); camera.aspect = w / h; camera.updateProjectionMatrix();
      }
      resize(); window.addEventListener('resize', resize);
      var pointer = { x: 0, y: 0 };
      window.addEventListener('pointermove', function (e) {
        pointer.x = e.clientX / window.innerWidth * 2 - 1; pointer.y = e.clientY / window.innerHeight * 2 - 1;
      }, { passive: true });
      var visible = true, running = false;
      function tick(t) {
        var time = t / 1000;
        group.rotation.y = time * 0.05;
        for (var j = 0; j < files.length; j++) files[j].mesh.position.y = files[j].y + Math.sin(time * 0.6 + files[j].phase) * 0.08;
        group.rotation.x += (-pointer.y * 0.0008 - group.rotation.x) * 0.03;
        group.rotation.z += (pointer.x * 0.0008 - group.rotation.z) * 0.03;
        camera.lookAt(0, 0, 0); renderer.render(scene, camera);
      }
      function start() { if (!running && visible && !document.hidden) { running = true; renderer.setAnimationLoop(tick); } }
      function stop() { if (running) { running = false; renderer.setAnimationLoop(null); } }
      var io = new IntersectionObserver(function (entries) { visible = entries[0].isIntersecting; if (visible) start(); else stop(); }, { threshold: 0.05 });
      io.observe(hero); document.addEventListener('visibilitychange', function () { if (document.hidden) stop(); else start(); });
      canvas.addEventListener('webglcontextlost', function (e) { e.preventDefault(); stop(); fallbackHero(); });
      start();
    } catch (e) { fallbackHero(); }
  }
  initHero();
})();