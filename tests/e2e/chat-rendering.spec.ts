import { test, expect } from "./authenticated";

test("model replies show sources, tables and code without executing HTML or loading tracking images", async ({ page }) => {
  let trackingRequests = 0;
  page.on("request", r => { if (r.url().includes("tracking.nemi.test")) trackingRequests++; });
  const reply = "### 公开资料\n\n**已读取**，参考[真实来源](https://example.com/)。\n\n| 地点 | 距离 |\n| --- | --- |\n| 起点 | 100米 |\n\n```sql\nSELECT 1;\n```\n\n[不安全链接](javascript:alert(1))\n\n![外部图片](https://tracking.nemi.test/pixel)\n\n<script>window.nemiMarkdownExecuted=true</script>\n<img src='https://tracking.nemi.test/html' onerror='window.nemiMarkdownExecuted=true'>";
  const conversation = { id: "render-only-fixture", title: "资料排版验收", updated_at: new Date().toISOString() };
  await page.route("**/api/v1/conversations", route => route.fulfill({ json: { conversations: [conversation] } }));
  await page.route("**/api/v1/conversations/render-only-fixture", route => route.fulfill({ json: { conversation, summary_through: 0, turns: [{ run_id: "render-only-run", text: "整理已读资料", reply, status: "SUCCEEDED", error: "", model: "protocol-fixture", steps: [], actions: [], files: [] }] } }));
  await page.goto("/");
  const rendered = page.locator(".chat-reply");
  await expect(rendered.getByRole("heading", { name: "公开资料", exact: true })).toBeVisible();
  await expect(rendered.getByRole("link", { name: "真实来源", exact: true })).toHaveAttribute("href", "https://example.com/");
  await expect(rendered.getByRole("cell", { name: "100米", exact: true })).toBeVisible();
  await expect(rendered.locator("pre code")).toHaveText("SELECT 1;\n");
  await expect(rendered.getByRole("link", { name: "不安全链接", exact: true })).toHaveCount(0);
  await expect(rendered.locator("img, script, iframe")).toHaveCount(0);
  expect(await page.evaluate(() => (window as unknown as Record<string, unknown>).nemiMarkdownExecuted)).toBeUndefined();
  expect(trackingRequests).toBe(0);
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(page.locator(".sidebar")).not.toBeInViewport();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});
