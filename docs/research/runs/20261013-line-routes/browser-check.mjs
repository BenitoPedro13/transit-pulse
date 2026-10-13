import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE ?? 'playwright');
const origin = process.env.TRANSIT_URL ?? 'http://localhost:3000';
const out = fileURLToPath(new URL('../../../tasks/assets/', import.meta.url));
const axe = process.env.AXE_SCRIPT ?? fileURLToPath(import.meta.resolve('axe-core/axe.min.js'));
const index = await (await fetch(origin + '/gtfs/index.json')).json();
const result = { layouts: [], patterns: [], accessibility: [], errors: [], performance: [] };
let browser = await chromium.launch({ channel: 'chromium', headless: true });

// Read the actual props supplied to VehicleLayer, without changing provider data or app behavior.
function layerProps() {
  const main = document.querySelector('main');
  let n = main[Object.keys(main).find(k => k.startsWith('__reactFiber$'))];
  while (n.return) n = n.return;
  const visit = n => {
    if (!n) return null;
    const p = n.memoizedProps;
    if (p && Array.isArray(p.vehicles) && Object.hasOwn(p, 'trails') && p.scheme) return p;
    return visit(n.child) || visit(n.sibling);
  };
  return visit(n.stateNode.current);
}
const source = layerProps.toString();
async function state(page) {
  return page.evaluate(src => {
    const p = eval('(' + src + ')')();
    return { line: p?.route?.line ?? null, pattern: p?.route?.pattern.id ?? null, stops: p?.route?.stops.length ?? 0,
      vehicles: p?.vehicles.length ?? 0, trails: p?.trails !== null, interpolated: p?.vehicles.filter(v => v.interpolated).length ?? 0 };
  }, source);
}
async function ready(page, line) {
  await page.getByRole('slider').waitFor({ timeout: 60000 });
  await page.waitForFunction(({ src, line }) => {
    const p = eval('(' + src + ')')();
    return p?.route?.line === line && p.vehicles.length > 0 && document.querySelector('#deckgl-overlay')?.width > 0;
  }, { src: source, line });
  await page.waitForTimeout(1000);
}
async function axeCheck(page, name) {
  await page.addScriptTag({ path: axe });
  const violations = await page.evaluate(async () => (await axe.run(document.querySelector('main'), {
    runOnly: { type: 'tag', values: ['wcag2a', 'wcag2aa', 'wcag21aa', 'wcag22aa'] },
  })).violations.map(v => ({ id: v.id, nodes: v.nodes.map(n => ({ target: n.target, summary: n.failureSummary })) })));
  result.accessibility.push({ name, violations });
  assert.deepEqual(violations, []);
}
async function selectLine(page, line) {
  await page.getByRole('searchbox', { name: 'Buscar linha' }).fill(line);
  await page.locator('section[aria-label="Ônibus"] button').filter({ hasText: new RegExp('^' + line + '\\b') }).click();
}
async function disclosureVisible(page) {
  // Check the badge's center against actual paint order, beyond DOM visibility alone.
  return page.getByText('Trajeto de referência · GTFS estático', { exact: true }).evaluate(e => {
    const r = e.getBoundingClientRect();
    const hit = document.elementsFromPoint(r.x + r.width / 2, r.y + r.height / 2);
    return r.top >= 0 && r.bottom <= innerHeight && !hit.some(n => n !== e && !n.contains(e) && n.closest('section[aria-label="Painel"]'));
  });
}
try {
  await fs.mkdir(out, { recursive: true });
  for (const scheme of ['dark', 'light']) {
    for (const viewport of [{ width: 1440, height: 900 }, { width: 390, height: 844 }]) {
      const mobile = viewport.width === 390;
      const context = await browser.newContext({ viewport, colorScheme: scheme, timezoneId: 'America/Sao_Paulo', hasTouch: mobile });
      const page = await context.newPage(); page.on('pageerror', e => result.errors.push(e.message));
      await page.goto(origin + '/?linha=22&t=2026-10-12T15:00Z'); await ready(page, '22');
      if (mobile) await page.getByRole('button', { name: 'Expandir painel' }).tap();
      await page.getByLabel('Destino e variante').waitFor();
      const options = await page.getByLabel('Destino e variante').evaluate(e => Array.from(e.options).map(o => o.value));
      assert.equal(options.length, 4);
      const initial = await state(page);
      await page.getByLabel('Destino e variante').focus(); await page.keyboard.press('ArrowUp'); await page.keyboard.press('Enter');
      const keyboardPattern = await page.getByLabel('Destino e variante').inputValue();
      await page.waitForFunction(({ src, id }) => eval('(' + src + ')')()?.route?.pattern.id === id, { src: source, id: keyboardPattern });
      // Measure inside the browser, so automation IPC does not count as application latency.
      const switchMs = await page.evaluate(({ src, id }) => new Promise(resolve => {
        const select = document.querySelector('#trajeto-destino');
        const start = performance.now(); select.value = id; select.dispatchEvent(new Event('change', { bubbles: true }));
        const tick = () => { if (eval('(' + src + ')')()?.route?.pattern.id === id) resolve(performance.now() - start); else requestAnimationFrame(tick); };
        requestAnimationFrame(tick);
      }), { src: source, id: options.find(id => id !== keyboardPattern) });
      assert.ok(switchMs < 1000);
      await page.getByText(/paradas · ver lista/).click();
      const after = await state(page);
      assert.equal(await page.getByRole('list', { name: 'Paradas do trajeto' }).locator('li').count(), after.stops);
      assert.equal(after.vehicles, initial.vehicles); assert.match(await page.locator('h1').innerText(), /REPLAY/);
      await axeCheck(page, `${viewport.width}-${scheme}-stops`);
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
      assert.equal(await disclosureVisible(page), true);
      await page.screenshot({ path: `${out}/line-routes-${viewport.width}x${viewport.height}-${scheme}-stops.png` });
      if (mobile) {
        await page.locator('#painel-conteudo').evaluate(e => e.scrollTop = e.scrollHeight);
        const collapse = page.getByRole('button', { name: 'Recolher painel' });
        assert.ok((await collapse.boundingBox()).height >= 44); await collapse.tap();
        assert.equal(await page.locator('section[aria-label="Painel"]').getAttribute('data-expanded'), 'false');
        assert.equal(new URL(page.url()).searchParams.get('linha'), '22');
        assert.equal(await disclosureVisible(page), true);
        await page.screenshot({ path: `${out}/line-routes-390x844-${scheme}-compact.png` });
        await page.getByRole('button', { name: 'Expandir painel' }).tap();
      }
      await page.getByRole('button', { name: 'Limpar linha' }).click();
      await page.getByRole('button', { name: 'Limpar linha' }).waitFor({ state: 'hidden' });
      assert.equal((await state(page)).line, null);
      await selectLine(page, '343'); await ready(page, '343');
      assert.equal((await state(page)).line, '343');
      await page.screenshot({ path: `${out}/line-routes-${viewport.width}x${viewport.height}-${scheme}-bus.png` });
      result.patterns.push({ scheme, width: viewport.width, variants: options.length, switchMs, keyboardPattern, stops: after.stops });
      result.layouts.push({ scheme, viewport, collapsedAfterScrolling: mobile, disclosureVisible: true, overflow: false });
      await context.close();
    }
  }
  console.log('LAYOUT/KEYBOARD/AXE PASS');

  // A delayed file must never leave the previous line drawn under the new URL.
  const race = await browser.newContext({ viewport: { width: 1440, height: 900 }, colorScheme: 'dark', timezoneId: 'America/Sao_Paulo' });
  const p = await race.newPage(); p.on('pageerror', e => result.errors.push(e.message));
  const delayedPath = index.lines.find(e => e.line === '343').file;
  await p.route('**' + delayedPath, async r => { await new Promise(resolve => setTimeout(resolve, 3500)); await r.continue().catch(() => {}); });
  await p.goto(origin + '/?linha=22&t=2026-10-12T15:00Z'); await ready(p, '22');
  await selectLine(p, '343'); await p.getByText('Carregando o trajeto de referência…').waitFor();
  assert.equal((await state(p)).line, null);
  await selectLine(p, '565'); await ready(p, '565'); await p.waitForTimeout(3500);
  assert.equal((await state(p)).line, '565'); result.race = { delayedPreviousLineRemoved: true, latestSelection: '565' };
  await race.close();

  for (const scenario of ['missing', 'failure']) {
    const c = await browser.newContext({ viewport: { width: 1440, height: 900 } }); const page = await c.newPage();
    if (scenario === 'failure') await page.route('**' + index.lines.find(e => e.line === '22').file, r => r.fulfill({ status: 503, body: 'validation failure' }));
    await page.goto(origin + `/?linha=${scenario === 'missing' ? 'ESPECIAL' : '22'}&t=2026-10-12T15:00Z`);
    await page.getByText(scenario === 'missing' ? 'Sem trajeto no GTFS para esta linha.' : 'Não foi possível carregar o trajeto de referência.').waitFor();
    await page.getByRole('slider').waitFor(); await page.waitForFunction(src => eval('(' + src + ')')()?.vehicles.length > 0, source);
    assert.equal((await state(page)).line, null); assert.match(await page.locator('h1').innerText(), /REPLAY/);
    if (scenario === 'failure') { await page.unroute('**' + index.lines.find(e => e.line === '22').file); await page.getByRole('button', { name: 'Tentar novamente' }).click(); await ready(page, '22'); }
    result[scenario === 'failure' ? 'assetFailure' : scenario] = { observedVehiclesPreserved: true, recovered: scenario === 'failure' };
    await c.close();
  }
  console.log('RACE/MISSING/RETRY PASS');

  const reduce = await browser.newContext({ viewport: { width: 1440, height: 900 }, reducedMotion: 'reduce', timezoneId: 'America/Sao_Paulo' });
  const rp = await reduce.newPage(); await rp.goto(origin + '/?linha=22&t=2026-10-12T15:00Z'); await ready(rp, '22');
  const reduced = await state(rp); assert.equal(reduced.trails, false); assert.equal(reduced.interpolated, 0);
  assert.equal(await rp.getByRole('button', { name: 'Reproduzir', exact: true }).count(), 1);
  result.reducedMotion = { noAutoplay: true, noTrails: true, noInterpolation: true, staticRoute: reduced.line };
  await reduce.close();

  // Keep frame measurements independent of the many short-lived layout/test contexts above.
  await browser.close();
  browser = await chromium.launch({ channel: 'chromium', headless: true });
  const perf = await browser.newContext({ viewport: { width: 1440, height: 900 }, colorScheme: 'dark', timezoneId: 'America/Sao_Paulo' });
  const pp = await perf.newPage(); let files = 0; pp.on('request', r => { if (r.url().includes('/gtfs/')) files++; });
  pp.on('pageerror', e => result.errors.push(e.message));
  await pp.goto(origin + '/?linha=22&t=2026-10-12T15:00Z'); await ready(pp, '22');
  result.gpu = await pp.evaluate(() => { const gl = document.createElement('canvas').getContext('webgl2'); const ext = gl.getExtension('WEBGL_debug_renderer_info'); return gl.getParameter(ext.UNMASKED_RENDERER_WEBGL); });
  await pp.locator('[aria-label="Velocidade 60 vezes"]').click();
  const initialRequests = files;
  for (let run = 0; run < 3; run++) {
    await pp.getByRole('slider').press('Home'); await pp.getByRole('slider').press('PageUp');
    await pp.getByRole('button', { name: 'Reproduzir', exact: true }).click();
    const fps = await pp.evaluate(() => new Promise(resolve => { let frames = 0; const start = performance.now(); const tick = () => { frames++; const elapsed = performance.now() - start; if (elapsed >= 10000) resolve(frames * 1000 / elapsed); else requestAnimationFrame(tick); }; requestAnimationFrame(tick); }));
    await pp.getByRole('button', { name: 'Pausar', exact: true }).click(); result.performance.push(fps);
    assert.equal((await state(pp)).line, '22');
  }
  result.medianFps = [...result.performance].sort((a, b) => a - b)[1];
  result.routeRequestsDuringPlayback = files - initialRequests;
  assert.ok(result.medianFps >= 30); assert.equal(result.routeRequestsDuringPlayback, 0);
  await pp.getByRole('button', { name: /AO VIVO/i }).click();
  await pp.getByRole('button', { name: 'Ao vivo', exact: true }).waitFor();
  assert.equal((await state(pp)).line, '22'); assert.equal(await disclosureVisible(pp), true);
  result.latestModeKeepsRoute = true;
  await perf.close(); assert.deepEqual(result.errors, []);
  console.log('PERFORMANCE PASS', result.performance, 'median', result.medianFps);
} catch (e) {
  result.failure = String(e.stack); console.error(e); process.exitCode = 1;
} finally {
  await fs.writeFile(`${out}/line-routes-browser-results.json`, JSON.stringify(result, null, 2) + '\n');
  await browser.close();
}
