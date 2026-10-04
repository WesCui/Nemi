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

test("harness compacts old context and restores the task plan after reload", async ({ page }) => {
  await page.getByRole("button", { name: "新对话", exact: true }).click();
  const history = page.getByLabel("对话内容", { exact: true });
  for (let i = 0; i < 6; i++) {
    await page.getByLabel("告诉妮米你想做的事").fill(i === 0 ? "预算1000元，不要订票" : i === 5 ? "制定任务计划" : `继续比较${i}`);
    await page.getByRole("button", { name: "发送消息", exact: true }).click();
    await expect(history.locator(".assistant > p").last()).toHaveText(i === 5 ? "已经记录工作计划。" : new RegExp(i === 0 ? "预算1000" : `继续比较${i}`));
    await expect(page.getByRole("button", { name: "停止本次任务", exact: true })).toHaveCount(0);
  }
  await expect(page.getByText("已整理较早的对话背景，完整记录仍保留在这里。", { exact: true })).toBeVisible();
  await page.reload();
  const plan = page.locator(".agent-plan");
  await plan.locator("summary").click();
  await expect(plan.getByText("比较方案", { exact: true })).toBeVisible();
  await expect(plan.getByText("进行中", { exact: true })).toBeVisible();
  const list = await (await page.request.get("/api/v1/conversations")).json();
  const detail = await (await page.request.get(`/api/v1/conversations/${list.conversations[0].id}`)).json();
  expect(detail.summary_through).toBe(3);
  expect(detail.turns).toHaveLength(6);
  expect(detail.turns[5].steps.map((s: { name: string }) => s.name)).toEqual(["context_summary", "model", "update_plan", "model"]);
  await page.screenshot({ path: "test-results/nemi-v07-harness.png", fullPage: true, animations: "disabled" });
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(page.locator(".sidebar")).not.toBeInViewport();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  await page.screenshot({ path: "test-results/nemi-v07-harness-mobile.png", fullPage: true, animations: "disabled" });
});

test("stop cancels an in-flight task and permits a new turn without reviving the old result", async ({ page }) => {
  await page.getByRole("button", { name: "新对话", exact: true }).click();
  await page.getByLabel("告诉妮米你想做的事").fill("执行可停止的任务");
  await page.getByRole("button", { name: "发送消息", exact: true }).click();
  await expect.poll(async () => {
    const list = await (await page.request.get("/api/v1/conversations")).json();
    const detail = await (await page.request.get(`/api/v1/conversations/${list.conversations[0].id}`)).json();
    return detail.turns[0].steps.some((s: { kind: string; status: string }) => s.kind === "MODEL" && s.status === "CALLING");
  }).toBe(true);
  await page.getByRole("button", { name: "停止本次任务", exact: true }).click();
  await expect(page.getByLabel("对话内容", { exact: true }).getByRole("alert")).toContainText("已停止本次任务");
  await page.getByLabel("告诉妮米你想做的事").fill("换一个问题");
  await page.getByRole("button", { name: "发送消息", exact: true }).click();
  await expect(page.getByLabel("对话内容", { exact: true }).locator(".assistant > p").last()).toHaveText("收到：换一个问题");
  await page.waitForTimeout(3500);
  await page.reload();
  const list = await (await page.request.get("/api/v1/conversations")).json();
  const detail = await (await page.request.get(`/api/v1/conversations/${list.conversations[0].id}`)).json();
  expect(detail.turns[0].error).toBe("AGENT_CANCELLED");
  expect(detail.turns[0].reply).toBe("");
  expect(detail.turns[0].steps[0].status).toBe("UNKNOWN");
  expect(detail.turns[1].status).toBe("SUCCEEDED");
});

test("connects an app inside the conversation, resumes the task, and reviews a group message", async ({ page }) => {
  let configured = false;
  try {
  await page.getByRole("button", { name: "新对话", exact: true }).click();
  const editor = page.getByLabel("告诉妮米你想做的事");
  await editor.fill("连接企业微信");
  await page.getByRole("button", { name: "发送消息", exact: true }).click();
  const card = page.getByLabel("应用连接卡片", { exact: true });
  await expect(card.getByRole("heading", { name: "连接企业微信群", exact: true })).toBeVisible();
  await card.getByLabel("接收群名称", { exact: true }).fill("隔离协议测试群");
  await card.getByLabel("机器人 Webhook", { exact: true }).fill("https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=fixture-never-sent");
  await card.getByRole("button", { name: "保存连接", exact: true }).click();
  configured = true;
  await expect(card.getByText("配置已保存", { exact: true })).toBeVisible();
  expect(new URL(page.url()).pathname).toBe("/");
  await card.getByRole("button", { name: "继续刚才的任务", exact: true }).click();
  await expect(page.getByLabel("对话内容", { exact: true }).locator(".assistant > p").last()).toContainText("应用连接已完成，请继续刚才的任务。");
  await editor.fill("发送群消息");
  await page.getByRole("button", { name: "发送消息", exact: true }).click();
  const message = page.getByLabel("群消息提案", { exact: true });
  await expect(message.getByText(/接收群：隔离协议测试群/)).toBeVisible();
  await expect(message.getByText("请核对这份工作安排。", { exact: true })).toBeVisible();
  await expect(message.getByRole("button", { name: "确认发送", exact: true })).toBeVisible();
  // Never send from browser tests to an external platform. Real outbound
  // confirmation and receipts are checked with a transport fixture in Go.
  await message.getByRole("button", { name: "取消发送", exact: true }).click();
  await expect(message.getByText("已取消发送", { exact: true })).toBeVisible();
  await page.reload();
  await expect(page.getByLabel("群消息提案", { exact: true }).getByText("已取消发送", { exact: true })).toBeVisible();
  const list = await (await page.request.get("/api/v1/conversations")).json();
  const detail = await (await page.request.get(`/api/v1/conversations/${list.conversations[0].id}`)).json();
  expect(JSON.stringify(detail)).not.toContain("fixture-never-sent");
  await page.screenshot({ path: "test-results/nemi-v07-agent-apps.png", fullPage: true, animations: "disabled" });
  } finally {
    if (configured) {
      const data = await (await page.request.get("/api/v1/connections")).json();
      const channel = data.channels.find((c: { id: string }) => c.id === "wecom");
      const result = await page.request.put("/api/v1/connections/wecom", { headers: { Origin: process.env.NEMI_BASE_URL!, "Idempotency-Key": crypto.randomUUID() }, data: { label: "", webhook: "", secret: "", enabled: false, expected_revision: channel.revision } });
      expect(result.status()).toBe(200);
    }
  }
});
