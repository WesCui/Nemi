import { test, expect } from "@playwright/test";

test.beforeEach(async ({ page }) => {
  await page.goto("/");
  await page.getByLabel("邀请口令").fill(process.env.NEMI_TEST_INVITE_CODE!);
  await page.getByRole("button", { name: "进入我的空间" }).click();
  await expect(page.getByRole("heading", { name: "有什么想交给妮米？" })).toBeVisible();
});

test("homepage sends LLM messages, retains context and reloads replies without creating matters", async ({ page }) => {
  const before = await (await page.request.get("/api/v1/dashboard")).json();
  await page.getByRole("button", { name: "新对话", exact: true }).click();
  const first = `我叫小林 ${Date.now()}`;
  await page.getByLabel("告诉妮米你想做的事").fill(first);
  await page.getByRole("button", { name: "发送消息", exact: true }).click();
  await expect(page.getByRole("dialog", { name: "记一件事" })).toHaveCount(0);
  const history = page.getByLabel("对话内容", { exact: true });
  await expect(history.locator(".assistant p").first()).toHaveText(`收到：${first}`);
  await page.getByLabel("告诉妮米你想做的事").fill("我叫什么？");
  await page.getByLabel("告诉妮米你想做的事").press("Enter");
  await expect(history.locator(".assistant p").last()).toHaveText(`收到：我叫什么？；前文：${first}`);
  await page.reload();
  await expect(page.getByLabel("对话内容", { exact: true }).locator(".assistant p").last()).toContainText(first);
  const after = await (await page.request.get("/api/v1/dashboard")).json();
  expect(after.matters.length).toBe(before.matters.length);
  await page.setViewportSize({ width: 390, height: 844 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  await page.screenshot({ path: "test-results/nemi-v05-chat-mobile.png", fullPage: true });
});

test("missing key prompts configuration and never posts a replacement run", async ({ page }) => {
  const data = await (await page.request.get("/api/v1/models")).json();
  await page.route("**/api/v1/models", async (route) => {
    if (route.request().method() !== "GET") return route.continue();
    await route.fulfill({ json: { ...data, models: [], default_id: "", server_available: false } });
  });
  let submitted = 0;
  page.on("request", (r) => { if (r.url().endsWith("/api/v1/chat/messages") && r.method() === "POST") submitted++; });
  await page.reload();
  await page.getByLabel("告诉妮米你想做的事").fill("帮我安排晚饭");
  await page.getByRole("button", { name: "发送消息", exact: true }).click();
  await expect(page.getByRole("dialog", { name: "我的模型", exact: true })).toBeVisible();
  expect(submitted).toBe(0);
  await expect(page.getByText(/演示生成|本地演示|不调用模型/)).toHaveCount(0);
  await page.unrouteAll({ behavior: "wait" });
});

test("provider failure remains visible and is not replaced with a canned answer", async ({ page }) => {
  await page.getByRole("button", { name: "新对话", exact: true }).click();
  await page.getByLabel("告诉妮米你想做的事").fill("触发认证失败");
  await page.getByRole("button", { name: "发送消息", exact: true }).click();
  await expect(page.getByLabel("对话内容", { exact: true }).getByRole("alert")).toHaveText("密钥未通过认证，请检查后重新配置。");
  const list = await (await page.request.get("/api/v1/conversations")).json();
  const detail = await (await page.request.get(`/api/v1/conversations/${list.conversations[0].id}`)).json();
  expect(detail.turns[0].reply).toBe("");
  expect(detail.turns[0].status).toBe("FAILED");
});

test("agent proposes a real action, persists confirmation, and reads the created matter through a tool", async ({ page }) => {
  await page.getByRole("button", { name: "新对话", exact: true }).click();
  const before = await (await page.request.get("/api/v1/dashboard")).json();
  const title = `整理书房 ${Date.now()}`;
  await page.getByLabel("告诉妮米你想做的事").fill(`帮我创建一个事项：${title}`);
  await page.getByRole("button", { name: "发送消息", exact: true }).click();
  const card = page.getByLabel("事项提案", { exact: true });
  await expect(card.getByRole("heading", { name: title })).toBeVisible();
  await expect(card.getByRole("button", { name: "确认创建" })).toBeVisible();
  await expect(page.getByLabel("对话内容", { exact: true }).locator(".assistant > p").last()).toHaveText("已准备好提案，请确认后创建。");
  const pending = await (await page.request.get("/api/v1/dashboard")).json();
  expect(pending.matters.length).toBe(before.matters.length);
  await page.reload();
  await expect(card.getByRole("button", { name: "确认创建" })).toBeVisible();
  await page.getByText("执行过程 · 3 步", { exact: true }).click();
  await expect(page.getByText("准备事项与提醒提案", { exact: true })).toBeVisible();
  await page.screenshot({ path: "test-results/nemi-v06-agent-proposal.png", fullPage: true });
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(page.locator(".sidebar")).not.toBeInViewport();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  await page.screenshot({ path: "test-results/nemi-v06-agent-proposal-mobile.png", fullPage: true, animations: "disabled" });
  await card.getByRole("button", { name: "确认创建" }).click();
  await expect(card.getByText("已确认并保存", { exact: true })).toBeVisible();
  const after = await (await page.request.get("/api/v1/dashboard")).json();
  const matters = after.matters.filter((m: { title: string }) => m.title === title);
  expect(matters).toHaveLength(1);
  expect(after.reminders.some((r: { matter_id: string }) => r.matter_id === matters[0].id)).toBe(true);
  await page.getByLabel("告诉妮米你想做的事").fill("查一下我的事项");
  await page.getByRole("button", { name: "发送消息", exact: true }).click();
  await expect(page.getByLabel("对话内容", { exact: true }).locator(".assistant > p").last()).toContainText(title);
  const list = await (await page.request.get("/api/v1/conversations")).json();
  const detail = await (await page.request.get(`/api/v1/conversations/${list.conversations[0].id}`)).json();
  expect(detail.turns[1].steps.map((s: { name: string }) => s.name)).toEqual(["model", "list_matters", "model"]);
  await page.setViewportSize({ width: 390, height: 844 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  await page.screenshot({ path: "test-results/nemi-v06-agent-mobile.png", fullPage: true, animations: "disabled" });
});
