import { test, expect } from "./authenticated";

test.beforeEach(async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "有什么想交给妮米？" })).toBeVisible();
});
test("homepage personal models persist without revealing credentials or invoking providers", async ({ page }) => {
  const label = `测试配置 ${Date.now()}`;
  let id = "";
  try {
    await expect(page.getByText(/私有开发体验版|开发体验版 · 仅本人/)).toHaveCount(0);
    await page.getByRole("button", { name: "模型配置", exact: true }).click();
    const modal = page.getByRole("dialog", { name: "我的模型", exact: true });
    if (await modal.getByRole("button", { name: "添加模型", exact: true }).isVisible()) await modal.getByRole("button", { name: "添加模型", exact: true }).click();
    await modal.getByLabel("服务商", { exact: true }).selectOption("kimi");
    await modal.getByLabel("配置名称", { exact: true }).fill(label);
    await modal.getByLabel("模型 ID", { exact: true }).fill("kimi-k2.6");
    await modal.getByLabel("API Key", { exact: true }).fill("fixture-not-a-real-credential");
    await modal.getByLabel("输入单价 · 元 / 百万 Token").fill("2");
    await modal.getByLabel("输出单价 · 元 / 百万 Token").fill("4");
    await modal.getByRole("button", { name: "保存配置", exact: true }).click();
    const row = modal.locator(".model-row").filter({ hasText: label });
    await expect(row).toBeVisible(); await expect(row.getByText("未验证", { exact: true })).toBeVisible();
    const models = await page.request.get("/api/v1/models");
    const text = await models.text(); expect(text).not.toContain("fixture-not-a-real-credential"); expect(text).not.toContain('"credential"');
    id = JSON.parse(text).models.find((m: { label: string }) => m.label === label).id;
    await modal.getByLabel("首页默认模型", { exact: true }).selectOption(id);
    await row.getByRole("button", { name: "检查连接", exact: true }).click();
    await expect(row.getByRole("button", { name: "确认检查", exact: true })).toBeDisabled();
    // No paid call is made with this placeholder key.
    await modal.getByRole("button", { name: "关闭", exact: true }).click();
    await page.reload();
    await expect(page.getByRole("button", { name: "模型配置", exact: true })).toHaveText(label);
    await page.getByRole("complementary").getByRole("button", { name: "记一件事", exact: true }).click();
    const create = page.getByRole("dialog", { name: "记一件事", exact: true });
    await expect(create.getByLabel("整理使用的模型")).toHaveValue(id);
    await create.getByRole("button", { name: "关闭", exact: true }).click();
    await page.setViewportSize({ width: 390, height: 844 });
    await page.getByRole("button", { name: "模型配置", exact: true }).click();
    await page.screenshot({ path: "test-results/nemi-v04-models-mobile.png", fullPage: true });
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  } finally {
    if (id) await page.request.delete(`/api/v1/models/${id}`, { headers: { Origin: process.env.NEMI_BASE_URL!, "Idempotency-Key": crypto.randomUUID() }, data: {} });
  }
});
test("domestic application credentials save locally with no automatic sends", async ({ page }) => {
  await page.getByRole("button", { name: "连接应用", exact: true }).click();
  await page.locator(".application-row").filter({ hasText: "企业微信" }).click();
  const settings = page.locator(".bot-settings");
  await settings.getByLabel("接收群名称", { exact: true }).fill("仅配置验收群");
  await settings.getByLabel("机器人 Webhook", { exact: true }).fill("https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=fixture-not-a-real-webhook");
  await settings.getByRole("button", { name: "保存连接", exact: true }).click();
  await expect(page.locator(".recipient")).toContainText("仅配置验收群");
  const response = await page.request.get("/api/v1/connections");
  const text = await response.text(); expect(text).not.toContain("fixture-not-a-real-webhook"); expect(text).not.toContain("webhook");
  const channel = JSON.parse(text).channels.find((c: { id: string }) => c.id === "wecom");
  expect(channel.verified_at).toBeNull();
  await page.getByRole("button", { name: "填写联调消息", exact: true }).click();
  await expect(page.getByRole("button", { name: "确认发送", exact: true })).toBeDisabled();
  await settings.getByRole("button", { name: "修改配置", exact: true }).click();
  await expect(settings.getByLabel("机器人 Webhook", { exact: true })).toHaveValue("");
  await settings.getByRole("button", { name: "停用连接", exact: true }).click();
  await page.locator(".application-row").filter({ hasText: "飞书" }).click();
  await expect(page.getByRole("heading", { name: "读取飞书文档", exact: true })).toBeVisible();
  await expect(page.getByLabel("App Secret", { exact: true })).toHaveAttribute("type", "password");
  await page.screenshot({ path: "test-results/nemi-v04-feishu-setup.png", fullPage: true });
});
