// Read-only verification of an actual, freshly initialized ARTEX server.
// This does NOT cover authenticated workflows or live LLM inference.
// Usage: node tools/check_runtime_readonly.mjs --base-url http://127.0.0.1:18787 --output <artifact-dir>
// ARTEX_PLAYWRIGHT_MODULE may point to an externally installed playwright-core.
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';
import { createRequire } from 'node:module';
const require = createRequire(import.meta.url);
const { chromium } = require(process.env.ARTEX_PLAYWRIGHT_MODULE || 'playwright-core');
const args = process.argv.slice(2);
function arg(name, fallback) {
  const at = args.indexOf(name);
  if (at === -1) return fallback;
  if (!args[at + 1]) throw new Error(`Missing value for ${name}`);
  return args[at + 1];
}
const base = new URL(arg('--base-url', 'http://127.0.0.1:18787'));
assert(['127.0.0.1', 'localhost', '[::1]'].includes(base.hostname), 'Only a loopback test instance is allowed');
const output = path.resolve(arg('--output', 'runtime-readonly-artifacts'));
await fs.mkdir(output, { recursive: true });
const report = {
  started_at: new Date().toISOString(), base_url: base.origin,
  scope: 'Fresh-instance startup, unauthenticated route guards and client-side negative validation only',
  authenticated_workflows_tested: false, live_llm_tested: false,
  http: [], browsers: [], status: 'running',
};
const routes = ['/dashboard', '/chat', '/function/tasks', '/function/assets', '/function/findings',
  '/function/traffic', '/function/workspace', '/system/agents', '/system/skills', '/system/tools',
  '/system/llm', '/system/settings', '/system/intercept', '/system/notify'];
try {
  for (const [endpoint, expected] of [['/api/health', 200], ['/api/auth/status', 200],
    ['/api/tasks', 401], ['/api/agents', 401], ['/api/skills', 401]]) {
    const response = await fetch(new URL(endpoint, base), { signal: AbortSignal.timeout(10000) });
    const body = await response.json();
    assert.equal(response.status, expected, endpoint);
    if (endpoint === '/api/health') assert.equal(body.ok, true);
    if (endpoint === '/api/auth/status') assert.equal(body.initialized, false, 'Requires a fresh disposable database; never resets existing passwords');
    if (expected === 401) assert.equal(body.error, '권한 없음');
    report.http.push({ endpoint, status: response.status, body, pass: true });
  }
  for (const channel of ['chrome', 'msedge']) {
    const browser = await chromium.launch({ channel, headless: true });
    const item = { channel, version: browser.version(), layouts: [], routes: [], page_errors: [], write_requests: 0 };
    report.browsers.push(item);
    try {
      const context = await browser.newContext({ locale: 'ko-KR' });
      // Invalid-form checks must never initialize an account or write any API data.
      await context.route('**/api/**', route => {
        if (route.request().method() !== 'GET') {
          item.write_requests += 1;
          return route.abort();
        }
        return route.continue();
      });
      const page = await context.newPage();
      page.setDefaultTimeout(10000);
      page.on('pageerror', error => item.page_errors.push(error.message));
      for (const viewport of [{ width: 1440, height: 1000 }, { width: 390, height: 844 }]) {
        await page.setViewportSize(viewport);
        await page.goto(new URL('/login', base).href);
        await page.getByRole('heading', { name: '초기 비밀번호 설정' }).waitFor();
        const submit = page.getByRole('button', { name: '비밀번호 설정 후 로그인' });
        assert.equal(await submit.isDisabled(), true);
        const dimensions = await page.evaluate(() => ({ lang: document.documentElement.lang,
          width: innerWidth, scrollWidth: document.documentElement.scrollWidth }));
        assert.equal(dimensions.lang, 'ko');
        assert(dimensions.scrollWidth <= dimensions.width, 'Horizontal overflow');
        await page.screenshot({ path: path.join(output, `${channel}-${viewport.width}-setup.png`), fullPage: true });
        await page.getByLabel('새 비밀번호', { exact: true }).fill('invalid-test');
        await page.getByLabel('비밀번호 확인', { exact: true }).fill('mismatch');
        await submit.click();
        await page.getByText('입력한 두 비밀번호가 일치하지 않습니다', { exact: true }).waitFor();
        await page.getByLabel('새 비밀번호', { exact: true }).fill('short');
        await page.getByLabel('비밀번호 확인', { exact: true }).fill('short');
        await submit.click();
        await page.getByText('비밀번호는 8자 이상이어야 합니다', { exact: true }).waitFor();
        await page.screenshot({ path: path.join(output, `${channel}-${viewport.width}-validation.png`), fullPage: true });
        item.layouts.push({ ...viewport, ...dimensions, empty_disabled: true, mismatch: 'pass', short_password: 'pass' });
      }
      for (const route of routes) {
        await page.goto(new URL(route, base).href);
        await page.getByRole('heading', { name: '초기 비밀번호 설정' }).waitFor();
        assert.equal(new URL(page.url()).pathname.replace(/\/$/, ''), '/setup');
        item.routes.push({ path: route, redirected_to: '/setup', pass: true });
      }
      assert.equal(item.write_requests, 0);
      assert.deepEqual(item.page_errors, []);
      console.log(`PASS ${channel} ${item.version}: two viewport layouts, form guards, ${routes.length} unauthenticated routes`);
    } finally {
      await browser.close();
    }
  }
  report.status = 'pass';
} catch (error) {
  report.status = 'fail';
  report.error = error.message;
  process.exitCode = 1;
} finally {
  report.finished_at = new Date().toISOString();
  await fs.writeFile(path.join(output, 'browser-runtime.json'), JSON.stringify(report, null, 2) + '\n', 'utf8');
  console.log(JSON.stringify({ status: report.status, authenticated_workflows_tested: false, live_llm_tested: false, error: report.error }));
}
