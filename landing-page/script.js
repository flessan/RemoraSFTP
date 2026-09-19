/* ============================================================
   RemoraSFTP landing page - redesigned
   nav · scroll spy · reveal · counters · copy/toast · theme
   OS detection · FAQ · live terminal · command palette
   Three.js hero (unchanged behavior, graceful fallback)
   ============================================================ */
(function () {
  'use strict';

  var THREE_CDN = 'https://cdn.jsdelivr.net/npm/three@0.160.0/build/three.module.js';
  var GH_URL = 'https://github.com/34labs/RemoraSFTP-dev';
  var RELEASES_URL = GH_URL + '/releases';

  var reducedMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches;

  function $(sel, ctx) { return (ctx || document).querySelector(sel); }
  function $all(sel, ctx) { return Array.prototype.slice.call((ctx || document).querySelectorAll(sel)); }

  /* ---------------- theme ---------------- */

  var root = document.documentElement;
  var metaTheme = $('#meta-theme');

  function applyTheme(theme) {
    root.setAttribute('data-theme', theme);
    if (metaTheme) metaTheme.setAttribute('content', theme === 'dark' ? '#0a1219' : '#f5f8f9');
    try { localStorage.setItem('remora-theme', theme); } catch (e) { }
  }
  try {
    var savedTheme = localStorage.getItem('remora-theme');
    if (savedTheme === 'dark' || savedTheme === 'light') applyTheme(savedTheme);
  } catch (e) { }

  var themeBtn = $('#theme-toggle');
  if (themeBtn) {
    themeBtn.addEventListener('click', function () {
      applyTheme(root.getAttribute('data-theme') === 'dark' ? 'light' : 'dark');
    });
  }

  /* ---------------- toast ---------------- */

  var toastEl = $('#toast');
  var toastTimer = null;
  function toast(msg) {
    if (!toastEl) return;
    toastEl.textContent = msg;
    toastEl.classList.add('show');
    if (toastTimer) clearTimeout(toastTimer);
    toastTimer = setTimeout(function () { toastEl.classList.remove('show'); }, 2000);
  }

  /* ---------------- copy to clipboard ---------------- */

  function copyText(text, msg) {
    function done() { toast(msg || 'Copied to clipboard'); }
    function legacy() {
      var ta = document.createElement('textarea');
      ta.value = text;
      ta.style.position = 'fixed';
      ta.style.opacity = '0';
      document.body.appendChild(ta);
      ta.select();
      try { document.execCommand('copy'); done(); }
      catch (e) { toast('Copy failed - select the text manually'); }
      document.body.removeChild(ta);
    }
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(text).then(done, legacy);
    } else legacy();
  }

  $all('.copy-btn').forEach(function (btn) {
    btn.addEventListener('click', function () {
      var block = btn.closest('.code');
      var pre = block ? block.querySelector('pre') : null;
      if (!pre) return;
      copyText(pre.innerText, 'Copied to clipboard');
      var label = btn.querySelector('span');
      if (label) {
        var old = label.textContent;
        label.textContent = 'Copied ✓';
        btn.classList.add('copied');
        setTimeout(function () { label.textContent = old; btn.classList.remove('copied'); }, 1600);
      }
    });
  });

  /* ---------------- nav: scrolled state + burger ---------------- */

  var nav = $('.nav');
  var burger = $('#nav-burger');
  var navLinks = $('#nav-links');
  var progressBar = $('#progress-bar');
  var toTop = $('#to-top');

  function onScroll() {
    if (nav) nav.classList.toggle('scrolled', window.scrollY > 8);
    var max = document.documentElement.scrollHeight - document.documentElement.clientHeight;
    var p = max > 0 ? Math.min(1, window.scrollY / max) : 0;
    if (progressBar) progressBar.style.transform = 'scaleX(' + p + ')';
    if (toTop) toTop.classList.toggle('show', window.scrollY > 600);
  }
  window.addEventListener('scroll', onScroll, { passive: true });
  onScroll();

  if (toTop) {
    toTop.addEventListener('click', function () {
      window.scrollTo({ top: 0, behavior: reducedMotion ? 'auto' : 'smooth' });
    });
  }

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
      if (e.target.tagName === 'A') closeNav();
    });
  }

  /* ---------------- scroll spy ---------------- */

  var spyLinks = {};
  $all('.nav-links a').forEach(function (a) {
    var href = a.getAttribute('href');
    if (href && href.charAt(0) === '#') spyLinks[href.slice(1)] = a;
  });

  if ('IntersectionObserver' in window) {
    var spy = new IntersectionObserver(function (entries) {
      entries.forEach(function (en) {
        if (!en.isIntersecting) return;
        var link = spyLinks[en.target.id];
        if (!link) return;
        $all('.nav-links a.active').forEach(function (el) { el.classList.remove('active'); });
        link.classList.add('active');
      });
    }, { rootMargin: '-40% 0px -55% 0px', threshold: 0 });
    Object.keys(spyLinks).forEach(function (id) {
      var s = document.getElementById(id);
      if (s) spy.observe(s);
    });
  }

  /* ---------------- scroll reveal ---------------- */

  var revealEls = $all('.reveal');
  if (reducedMotion || !('IntersectionObserver' in window)) {
    revealEls.forEach(function (el) { el.classList.add('visible'); });
  } else {
    var revealIO = new IntersectionObserver(function (entries) {
      entries.forEach(function (entry) {
        if (entry.isIntersecting) {
          entry.target.classList.add('visible');
          revealIO.unobserve(entry.target);
        }
      });
    }, { threshold: 0.12, rootMargin: '0px 0px -40px 0px' });
    revealEls.forEach(function (el) { revealIO.observe(el); });
  }

  /* ---------------- animated counters ---------------- */

  function runCounter(el) {
    var target = parseInt(el.getAttribute('data-count'), 10) || 0;
    var pre = el.getAttribute('data-prefix') || '';
    var suf = el.getAttribute('data-suffix') || '';
    if (reducedMotion || target === 0) { el.textContent = pre + target + suf; return; }
    var t0 = null, dur = 1100;
    function frame(ts) {
      if (!t0) t0 = ts;
      var p = Math.min(1, (ts - t0) / dur);
      var eased = 1 - Math.pow(1 - p, 3);
      el.textContent = pre + Math.round(eased * target) + suf;
      if (p < 1) requestAnimationFrame(frame);
    }
    requestAnimationFrame(frame);
  }

  var counterIO = 'IntersectionObserver' in window ? new IntersectionObserver(function (entries) {
    entries.forEach(function (entry) {
      if (!entry.isIntersecting) return;
      $all('[data-count]', entry.target).forEach(runCounter);
      counterIO.unobserve(entry.target);
    });
  }, { threshold: 0.4 }) : null;

  $all('.stat-chips').forEach(function (group) {
    if (counterIO) counterIO.observe(group);
    else $all('[data-count]', group).forEach(runCounter);
  });

  /* ---------------- FAQ accordion ---------------- */

  $all('.faq-item').forEach(function (item) {
    var q = $('.faq-q', item);
    if (!q) return;
    q.addEventListener('click', function () {
      var open = item.classList.toggle('open');
      q.setAttribute('aria-expanded', open ? 'true' : 'false');
    });
  });

  /* ---------------- OS detection for downloads ---------------- */

  (function detectOS() {
    var ua = navigator.userAgent || '';
    var os = null;
    if (/Windows/i.test(ua)) os = 'windows';
    else if (/Android/i.test(ua)) os = 'linux';
    else if (/iPhone|iPad|iPod|Mac OS X|Macintosh/i.test(ua)) os = 'macos';
    else if (/Linux/i.test(ua)) os = 'linux';
    if (!os) return;
    $all('.dl-card[data-os]').forEach(function (card) {
      var c = card.getAttribute('data-os');
      var match =
        (os === 'windows' && c === 'windows') ||
        (os === 'macos' && c === 'macos-arm') || // badge Apple Silicon by default
        (os === 'linux' && c === 'linux');
      if (match) {
        card.classList.add('recommended');
        var b = $('.rec-badge', card);
        if (b) b.hidden = false;
      }
    });
  })();

  /* ---------------- live typing terminal ---------------- */

  var termWrap = $('#term-wrap');
  var termText = $('#term-text');

  var LIVE_SCRIPT = [
    { c: 'remorasftp connect production' },
    { o: '✓ host key verified - saved to local trust store' },
    { o: '  connected · sftp · 10.40.0.12:/srv' },
    { c: 'remorasftp put ./build.tar.gz /srv/backups/' },
    { o: '↑ build.tar.gz  [████████████████████] 100% · 3.1 MB/s' },
    { c: 'remorasftp search /srv/web --name "*.log"' },
    { o: '  /srv/web/deploy.log   48 KB' },
    { o: '  /srv/web/error.log    12 KB' },
    { c: 'remorasftp get /srv/web/release-notes.md .' },
    { o: '✓ release-notes.md → ./release-notes.md (18 KB)' }
  ];

  var termTimer = null;
  var termRunning = false;

  function runLiveTerm() {
    if (termRunning || reducedMotion || !termText) return;
    termRunning = true;
    termText.textContent = '';
    var li = 0, ci = 0, buf = '';
    function step() {
      if (!termRunning) return;
      if (li >= LIVE_SCRIPT.length) {
        termTimer = setTimeout(function () {
          termRunning = false;
          runLiveTerm();
        }, 3800);
        return;
      }
      var line = LIVE_SCRIPT[li];
      if (line.c) {
        if (ci === 0) buf += '$ ';
        if (ci < line.c.length) {
          buf += line.c[ci++];
          termText.textContent = buf;
          termTimer = setTimeout(step, 34 + Math.random() * 40);
        } else {
          buf += '\n'; ci = 0; li++;
          termText.textContent = buf;
          termTimer = setTimeout(step, 380);
        }
      } else {
        buf += line.o + '\n'; li++;
        termText.textContent = buf;
        termTimer = setTimeout(step, 300);
      }
    }
    step();
  }
  function stopLiveTerm() {
    termRunning = false;
    if (termTimer) { clearTimeout(termTimer); termTimer = null; }
  }

  if (termText && reducedMotion) {
    termText.textContent = LIVE_SCRIPT.map(function (l) {
      return l.c ? '$ ' + l.c : l.o;
    }).join('\n');
  } else if (termWrap && 'IntersectionObserver' in window) {
    var termIO = new IntersectionObserver(function (entries) {
      entries.forEach(function (en) {
        if (en.isIntersecting) runLiveTerm();
        else stopLiveTerm();
      });
    }, { threshold: 0.25 });
    termIO.observe(termWrap);
  }

  /* ---------------- command palette ---------------- */

  var overlay = $('#palette-overlay');
  var paletteInput = $('#palette-input');
  var paletteList = $('#palette-list');
  var paletteOpenBtn = $('#palette-open');
  var lastFocus = null;
  var filtered = [];
  var sel = 0;

  function scrollToSel(sel2) {
    var el = document.querySelector(sel2);
    if (el) el.scrollIntoView({ behavior: reducedMotion ? 'auto' : 'smooth' });
  }

  var PALETTE_ACTIONS = [
    { label: 'Go to Overview', tag: 'Section', run: function () { scrollToSel('#what'); } },
    { label: 'Go to Features', tag: 'Section', run: function () { scrollToSel('#features'); } },
    { label: 'Go to Protocols', tag: 'Section', run: function () { scrollToSel('#protocols'); } },
    { label: 'Go to Terminal & CLI', tag: 'Section', run: function () { scrollToSel('#tui'); } },
    { label: 'Go to Security', tag: 'Section', run: function () { scrollToSel('#security'); } },
    { label: 'Go to Comparison', tag: 'Section', run: function () { scrollToSel('#compare'); } },
    { label: 'Go to FAQ', tag: 'Section', run: function () { scrollToSel('#faq'); } },
    { label: 'Go to Download', tag: 'Section', run: function () { scrollToSel('#download'); } },
    {
      label: 'Toggle dark / light theme', tag: 'Action', run: function () {
        applyTheme(root.getAttribute('data-theme') === 'dark' ? 'light' : 'dark');
      }
    },
    {
      label: 'Copy: git clone RemoraSFTP', tag: 'Copy', run: function () {
        copyText('git clone ' + GH_URL, 'Clone command copied');
      }
    },
    {
      label: 'Open GitHub repository', tag: 'Link', run: function () {
        window.open(GH_URL, '_blank', 'noopener');
      }
    },
    {
      label: 'Open releases page', tag: 'Link', run: function () {
        window.open(RELEASES_URL, '_blank', 'noopener');
      }
    }
  ];

  function renderPalette(query) {
    if (!paletteList) return;
    var q = (query || '').trim().toLowerCase();
    filtered = PALETTE_ACTIONS.filter(function (it) {
      return !q || (it.label + ' ' + it.tag).toLowerCase().indexOf(q) !== -1;
    });
    sel = 0;
    paletteList.innerHTML = '';
    if (!filtered.length) {
      var empty = document.createElement('li');
      empty.className = 'palette-empty';
      empty.textContent = 'No matching commands';
      paletteList.appendChild(empty);
      return;
    }
    filtered.forEach(function (it, i) {
      var li = document.createElement('li');
      li.className = 'palette-item' + (i === sel ? ' selected' : '');
      li.setAttribute('role', 'option');
      li.setAttribute('aria-selected', i === sel ? 'true' : 'false');
      var l = document.createElement('span');
      l.textContent = it.label;
      var h = document.createElement('span');
      h.className = 'palette-hint';
      h.textContent = it.tag;
      li.appendChild(l);
      li.appendChild(h);
      li.addEventListener('mouseenter', function () { setSel(i); });
      li.addEventListener('click', function () { execSel(); });
      paletteList.appendChild(li);
    });
  }

  function setSel(i) {
    sel = i;
    $all('.palette-item', paletteList).forEach(function (el, idx) {
      el.classList.toggle('selected', idx === sel);
      el.setAttribute('aria-selected', idx === sel ? 'true' : 'false');
    });
    var active = $all('.palette-item', paletteList)[sel];
    if (active && active.scrollIntoView) active.scrollIntoView({ block: 'nearest' });
  }

  function execSel() {
    var item = filtered[sel];
    closePalette();
    if (item) item.run();
  }

  function openPalette() {
    if (!overlay) return;
    lastFocus = document.activeElement;
    overlay.hidden = false;
    requestAnimationFrame(function () { overlay.classList.add('show'); });
    if (paletteInput) paletteInput.value = '';
    renderPalette('');
    document.body.style.overflow = 'hidden';
    setTimeout(function () { if (paletteInput) paletteInput.focus(); }, 0);
  }

  function closePalette() {
    if (!overlay || overlay.hidden) return;
    overlay.classList.remove('show');
    document.body.style.overflow = '';
    setTimeout(function () { overlay.hidden = true; }, 150);
    if (lastFocus && lastFocus.focus) lastFocus.focus();
  }

  if (overlay && paletteInput) {
    if (paletteOpenBtn) paletteOpenBtn.addEventListener('click', openPalette);

    overlay.addEventListener('click', function (e) {
      if (e.target === overlay) closePalette();
    });

    paletteInput.addEventListener('input', function () { renderPalette(paletteInput.value); });

    paletteInput.addEventListener('keydown', function (e) {
      if (!filtered.length) return;
      if (e.key === 'ArrowDown') { e.preventDefault(); setSel((sel + 1) % filtered.length); }
      else if (e.key === 'ArrowUp') { e.preventDefault(); setSel((sel - 1 + filtered.length) % filtered.length); }
      else if (e.key === 'Enter') { e.preventDefault(); execSel(); }
    });

    document.addEventListener('keydown', function (e) {
      if ((e.metaKey || e.ctrlKey) && (e.key === 'k' || e.key === 'K')) {
        e.preventDefault();
        if (overlay.hidden) openPalette();
        else closePalette();
      } else if (e.key === 'Escape') {
        if (!overlay.hidden) closePalette();
        else closeNav();
      }
    });
  }

  /* ---------------- hero 3D scene (unchanged) ---------------- */

  var heroSection = $('.hero');
  var heroVisual = $('#hero-visual');
  var canvas = $('#hero-canvas');
  var fallback = $('#hero-fallback');

  function showFallback() {
    if (canvas) canvas.style.display = 'none';
    if (fallback) fallback.hidden = false;
  }

  function webglAvailable() {
    try {
      var c = document.createElement('canvas');
      return !!(
        window.WebGLRenderingContext &&
        (c.getContext('webgl2') || c.getContext('webgl'))
      );
    } catch (err) {
      return false;
    }
  }

  async function initHero() {
    if (!heroSection || !canvas) return;
    if (!webglAvailable()) { showFallback(); return; }
    var THREE;
    try {
      THREE = await import(THREE_CDN);
    } catch (err) {
      showFallback();
      return;
    }
    buildScene(THREE);
  }

  function buildScene(THREE) {
    var renderer;
    try {
      renderer = new THREE.WebGLRenderer({
        canvas: canvas, antialias: true, alpha: true, powerPreference: 'low-power'
      });
    } catch (err) {
      showFallback();
      return;
    }

    renderer.setPixelRatio(
      Math.min(window.devicePixelRatio || 1, window.innerWidth < 768 ? 1.75 : 2)
    );

    var scene = new THREE.Scene();
    scene.fog = new THREE.Fog(0x0b141b, 9, 17);

    var camera = new THREE.PerspectiveCamera(42, 1, 0.1, 40);
    camera.position.set(0, 0, 6.6);

    scene.add(new THREE.HemisphereLight(0xbfd8dd, 0x14212b, 1.05));
    var key = new THREE.DirectionalLight(0xffffff, 1.15);
    key.position.set(4, 6, 3);
    scene.add(key);
    var fill = new THREE.DirectionalLight(0x63b9bf, 0.22);
    fill.position.set(-5, -2, -3);
    scene.add(fill);

    var group = new THREE.Group();
    scene.add(group);

    var palette = [0x63b9bf, 0x8fa8bd, 0xd9c9a3];

    var folderMat = new THREE.MeshLambertMaterial({ color: 0x3d566b });
    var folder = new THREE.Group();
    var folderBody = new THREE.Mesh(new THREE.BoxGeometry(1.7, 1.15, 0.22), folderMat);
    var folderTab = new THREE.Mesh(new THREE.BoxGeometry(0.62, 0.16, 0.22), folderMat);
    folderTab.position.set(-0.5, 0.66, 0);
    folder.add(folderBody, folderTab);
    group.add(folder);

    var small = window.innerWidth < 768;
    var innerCount = small ? 6 : 8;
    var outerCount = small ? 9 : 12;
    var files = [];
    var fileGeo = new THREE.BoxGeometry(0.55, 0.7, 0.06);

    function makeFile(radius, index, total, phase) {
      var angle = (index / total) * Math.PI * 2 + phase;
      var mat = new THREE.MeshLambertMaterial({
        color: palette[(index + (radius > 3 ? 1 : 0)) % palette.length]
      });
      var mesh = new THREE.Mesh(fileGeo, mat);
      var x = Math.cos(angle) * radius;
      var y = Math.sin(index * 2.4) * 0.45;
      var z = Math.sin(angle) * radius;
      mesh.position.set(x, y, z);
      mesh.rotation.y = angle + Math.PI / 2;
      mesh.rotation.z = (Math.sin(index * 1.7) - 0.5) * 0.18;
      group.add(mesh);
      files.push({ mesh: mesh, baseY: y, phase: index * 0.7 });
      return new THREE.Vector3(x, y, z);
    }

    var filePositions = [];
    for (var i = 0; i < innerCount; i++) filePositions.push(makeFile(2.3, i, innerCount, 0.35));
    for (var j = 0; j < outerCount; j++) filePositions.push(makeFile(3.5, j, outerCount, -0.2));

    var lineVerts = [];
    filePositions.forEach(function (p) { lineVerts.push(0, 0, 0, p.x, p.y, p.z); });
    var lineGeo = new THREE.BufferGeometry();
    lineGeo.setAttribute('position', new THREE.Float32BufferAttribute(lineVerts, 3));
    var lines = new THREE.LineSegments(lineGeo, new THREE.LineBasicMaterial({
      color: 0x63b9bf, transparent: true, opacity: 0.3
    }));
    group.add(lines);

    [
      { r: 2.9, tube: 0.008, op: 0.16, tilt: 1.42, roll: 0.18 },
      { r: 3.95, tube: 0.006, op: 0.11, tilt: 1.52, roll: -0.24 }
    ].forEach(function (cfg) {
      var ring = new THREE.Mesh(
        new THREE.TorusGeometry(cfg.r, cfg.tube, 6, 100),
        new THREE.MeshBasicMaterial({ color: 0x63b9bf, transparent: true, opacity: cfg.op })
      );
      ring.rotation.x = cfg.tilt;
      ring.rotation.y = cfg.roll;
      group.add(ring);
    });

    var dustCount = small ? 50 : 90;
    var dustPos = new Float32Array(dustCount * 3);
    for (var d = 0; d < dustCount; d++) {
      var v = new THREE.Vector3(
        Math.random() * 2 - 1, Math.random() * 2 - 1, Math.random() * 2 - 1
      ).normalize().multiplyScalar(5 + Math.random() * 4);
      dustPos[d * 3] = v.x;
      dustPos[d * 3 + 1] = v.y;
      dustPos[d * 3 + 2] = v.z;
    }
    var dustGeo = new THREE.BufferGeometry();
    dustGeo.setAttribute('position', new THREE.Float32BufferAttribute(dustPos, 3));
    var dust = new THREE.Points(dustGeo, new THREE.PointsMaterial({
      color: 0x8ed4d8, size: 0.045, transparent: true, opacity: 0.35, sizeAttenuation: true
    }));
    scene.add(dust);

    function resize() {
      var w = heroVisual.clientWidth;
      var h = heroVisual.clientHeight;
      if (!w || !h) return;
      renderer.setSize(w, h, false);
      camera.aspect = w / h;
      camera.updateProjectionMatrix();
    }
    resize();
    window.addEventListener('resize', resize);

    var pointer = { x: 0, y: 0 };
    var camOffset = { x: 0, y: 0 };
    var scrollP = 0;
    var visible = true;
    var running = false;
    var lastT = 0;

    window.addEventListener('pointermove', function (e) {
      pointer.x = (e.clientX / window.innerWidth) * 2 - 1;
      pointer.y = (e.clientY / window.innerHeight) * 2 - 1;
    }, { passive: true });

    function readScroll() {
      var range = heroSection.offsetHeight * 0.9 || 1;
      scrollP = Math.min(1, Math.max(0, window.scrollY / range));
    }
    window.addEventListener('scroll', readScroll, { passive: true });
    readScroll();

    function tick(t) {
      var time = t / 1000;
      var dt = Math.min(0.05, time - lastT || 0.016);
      lastT = time;

      group.rotation.y = time * 0.05 + scrollP * 0.3;
      group.rotation.x = -0.26 * scrollP;
      folder.position.y = Math.sin(time * 0.5) * 0.06;

      for (var i2 = 0; i2 < files.length; i2++) {
        var f = files[i2];
        f.mesh.position.y = f.baseY + Math.sin(time * 0.6 + f.phase) * 0.08;
      }
      dust.rotation.y = -time * 0.01;

      camOffset.x += (pointer.x * 0.55 - camOffset.x) * 0.04;
      camOffset.y += (-pointer.y * 0.35 - camOffset.y) * 0.04;
      camera.position.x = camOffset.x;
      camera.position.y = camOffset.y;
      camera.position.z = 6.6 + scrollP * 1.5;
      camera.lookAt(0, 0, 0);

      canvas.style.opacity = String(1 - scrollP * 0.65);
      renderer.render(scene, camera);
    }

    function start() {
      if (running || reducedMotion || !visible) return;
      running = true;
      lastT = 0;
      renderer.setAnimationLoop(tick);
    }
    function stop() {
      if (!running) return;
      running = false;
      renderer.setAnimationLoop(null);
    }

    if (reducedMotion) {
      group.rotation.y = 0.5;
      renderer.render(scene, camera);
      return;
    }

    var io = new IntersectionObserver(function (entries) {
      visible = entries[0].isIntersecting;
      if (visible && !document.hidden) start();
      else stop();
    }, { threshold: 0.05 });
    io.observe(heroSection);

    document.addEventListener('visibilitychange', function () {
      if (document.hidden) stop();
      else if (visible) start();
    });

    canvas.addEventListener('webglcontextlost', function (e) {
      e.preventDefault();
      stop();
      showFallback();
    });

    start();
  }

  initHero();
})();
