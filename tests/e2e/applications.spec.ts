import { test, expect } from "./authenticated";

test.beforeEach(async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "有什么想交给妮米？" })).toBeVisible();
});

test("application tasks return to the current chat without outside navigation or losing a draft", async ({ page }) => {
  await page.getByLabel("告诉妮米你想做的事").fill("请保留我正在写的目标。");
  let chatCalls = 0;
  page.on("request", r => { if (r.url().endsWith("/api/v1/chat/messages") && r.method() === "POST") chatCalls++; });
  await page.getByRole("button", { name: "连接应用", exact: true }).click();
  await expect(page.locator(".application-row")).toHaveCount(15);
  await expect(page.locator(".application-detail a[target='_blank']")).toHaveCount(0);
  await expect(page.locator(".application-detail input[type='password']")).toHaveCount(0);
  await page.getByRole("button", { name: "办公协作", exact: true }).click();
  await expect(page.locator(".application-row")).toHaveCount(6);
  await page.getByLabel("搜索应用").fill("腾讯");
  await expect(page.locator(".application-row")).toHaveCount(1);
  await expect(page.getByText(/粘贴网址不会让 Nemi 获得/)).toBeVisible();
  await expect(page.getByText("尚未接入", { exact: true }).first()).toBeVisible();
  await page.getByLabel("搜索应用").fill("企业微信");
  await page.getByRole("button", { name: "交给妮米", exact: true }).click();
  await expect(page.getByRole("heading", { name: "有什么想交给妮米？" })).toBeVisible();
  const composer = page.getByLabel("告诉妮米你想做的事");
  await expect(composer).toHaveValue(/请保留我正在写的目标。\n帮我把工作跟进安排/);
  await expect(composer).toBeFocused();
  expect(chatCalls).toBe(0);
  expect(page.url()).toBe(`${process.env.NEMI_BASE_URL}/`);
  await page.getByRole("button", { name: "连接应用", exact: true }).click();
  await page.screenshot({ path: "test-results/nemi-v011-conversational-apps.png", fullPage: true });
});

test("natural language disconnect reviews the connection and clears credentials only after approval", async ({ page }) => {
  const origin = process.env.NEMI_BASE_URL!;
  const channels = (await (await page.request.get("/api/v1/connections")).json()).channels;
  const revision = channels.find((c: { id: string }) => c.id === "wecom").revision;
  const save = await page.request.put("/api/v1/connections/wecom", {
    headers: { Origin: origin, "Idempotency-Key": crypto.randomUUID() },
    data: { label: "会话验收群", webhook: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=fixture-never-send", enabled: true, expected_revision: revision },
  });
  expect(save.status()).toBe(200);
  await page.getByLabel("告诉妮米你想做的事").fill("停用企业微信");
  await page.getByRole("button", { name: "发送消息", exact: true }).click();
  const proposal = page.getByLabel("停用连接提案").last();
  await expect(proposal.getByRole("button", { name: "确认停用" })).toBeVisible();
  await expect(proposal.getByText("连接：会话验收群")).toBeVisible();
  expect((await (await page.request.get("/api/v1/connections")).json()).channels.find((c: { id: string }) => c.id === "wecom").state).toBe("configured");
  await page.setViewportSize({ width: 390, height: 844 });
  await proposal.getByRole("button", { name: "确认停用" }).click();
  await expect(proposal.getByText("连接已停用", { exact: true })).toBeVisible();
  expect((await (await page.request.get("/api/v1/connections")).json()).channels.find((c: { id: string }) => c.id === "wecom").state).toBe("unconfigured");
  await page.reload();
  await expect(page.getByLabel("停用连接提案").last().getByText("连接已停用", { exact: true })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await page.screenshot({ path: "test-results/nemi-v011-disconnect-mobile.png", fullPage: true });
});

test("calendar task from an existing matter produces a real previewable downloadable file in chat", async ({ page }) => {
  const title = `验收·对话日历 ${Date.now()}`;
  const origin = process.env.NEMI_BASE_URL!;
  const at = new Date(Math.floor((Date.now() + 7 * 86400000) / 60000) * 60000).toISOString();
  const created = await page.request.post("/api/v1/matters", {
    headers: { Origin: origin, "Idempotency-Key": crypto.randomUUID() },
    data: { title, category: "life", timezone: "Asia/Shanghai", confirmed: true, deadline: at },
  });
  expect(created.status()).toBe(201);
  const matter = await created.json();
  await page.reload();
  await page.getByRole("button", { name: /^我的事项/ }).click();
  await page.locator(".matter-grid:visible").getByRole("heading", { name: title, exact: true }).click();
  await page.getByRole("button", { name: "让妮米生成日历文件" }).click();
  await expect(page.getByLabel("告诉妮米你想做的事")).toHaveValue(new RegExp(matter.id));
  await page.getByRole("button", { name: "发送消息", exact: true }).click();
  await expect(page.getByRole("button", { name: "预览 日程.ics" }).last()).toBeVisible();
  const href = await page.getByRole("link", { name: "下载 日程.ics" }).last().getAttribute("href");
  const result = await page.request.get(href!);
  expect(result.status()).toBe(200);
  expect(result.headers()["content-type"]).toContain("text/calendar");
  const content = await result.text();
  expect(content).toContain(`UID:${matter.id}@nemi.local`);
  expect(content).toContain(`DTSTART:${at.replace(/[-:]/g, "").replace(/\.\d{3}Z$/, "Z")}`);
  await page.getByRole("button", { name: "预览 日程.ics" }).last().click();
  await expect(page.getByRole("dialog").getByText(/需.*导入日历|请导入日历/)).toBeVisible();
  await page.getByRole("button", { name: "关闭资料预览" }).click();
  const download = page.waitForEvent("download");
  await page.getByRole("link", { name: "下载 日程.ics" }).last().click();
  expect((await download).suggestedFilename()).toBe("日程.ics");
  await page.reload();
  await expect(page.getByRole("button", { name: "预览 日程.ics" }).last()).toBeVisible();
  const finished = await page.request.patch(`/api/v1/matters/${matter.id}`, { headers: { Origin: origin, "Idempotency-Key": crypto.randomUUID() }, data: { expected_revision: 1, status: "COMPLETED" } });
  expect(finished.status()).toBe(200);
});
