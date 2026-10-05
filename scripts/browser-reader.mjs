import { chromium } from "playwright";
import { existsSync } from "node:fs";

// Only a fixed rendering operation is accepted, never model-written JavaScript.
let raw = "";
for await (const chunk of process.stdin) { raw += chunk; if (raw.length > 8192) process.exit(1); }
const request = JSON.parse(raw);
const target = new URL(request.url);
const gateway = new URL(request.gateway);
if (target.protocol !== "https:" || gateway.hostname !== "127.0.0.1" || gateway.pathname !== "/fetch") process.exit(1);
let browser;
const timeout = setTimeout(async () => { await browser?.close().catch(() => {}); process.exit(1); }, 28000);
try {
  browser = await chromium.launch({ channel: existsSync(chromium.executablePath()) ? "chromium" : "chrome", headless: true, args: ["--proxy-server=http://127.0.0.1:1", "--proxy-bypass-list=<-loopback>", "--disable-quic", "--force-webrtc-ip-handling-policy=disable_non_proxied_udp", "--disable-background-networking"] });
  const context = await browser.newContext({ serviceWorkers: "block", acceptDownloads: false, viewport: { width: 1280, height: 900 } });
  await context.routeWebSocket("**/*", socket => socket.close());
  await context.route("**/*", async route => {
    const source = route.request();
    try {
      const url = new URL(source.url());
      if (url.protocol !== "https:" || !["GET", "HEAD"].includes(source.method()) || ["image", "media", "font"].includes(source.resourceType())) return await route.abort();
      const response = await fetch(gateway, { method: "POST", headers: { "Content-Type": "application/json", Authorization: `Bearer ${request.token}` }, body: JSON.stringify({ url: url.href, method: source.method() }), signal: AbortSignal.timeout(10000) });
      if (!response.ok) return await route.abort();
      const data = await response.json();
      await route.fulfill({ status: data.status, headers: data.headers, body: Buffer.from(data.body, "base64") });
    } catch { await route.abort().catch(() => {}); }
  });
  const page = await context.newPage();
  page.on("popup", popup => popup.close().catch(() => {}));
  const response = await page.goto(target.href, { waitUntil: "domcontentloaded", timeout: 18000 });
  if (!response || response.status() < 200 || response.status() >= 300) throw new Error("read failed");
  await page.waitForTimeout(1500);
  const data = await page.evaluate(() => {
    const text = document.body?.innerText || "";
    return { url: location.href, title: document.title.slice(0, 160), text: Array.from(text).slice(0, 18000).join(""), truncated: Array.from(text).length > 18000,
      links: Array.from(document.querySelectorAll("a[href]")).filter(a => a.href.startsWith("https:") && a.innerText.trim()).slice(0, 15).map(a => ({ text: a.innerText.trim().slice(0, 80), url: a.href.slice(0, 2048) })) };
  });
  process.stdout.write(JSON.stringify(data));
} catch { process.exitCode = 1; }
finally { clearTimeout(timeout); await browser?.close().catch(() => {}); }
