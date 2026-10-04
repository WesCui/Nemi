import { test, expect } from "./authenticated";

test.beforeEach(async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "有什么想交给妮米？" })).toBeVisible();
});
test("domestic applications, map handoff, filters and calendar export", async ({
  page,
}) => {
  await page.getByRole("button", { name: "连接应用", exact: true }).click();
  await expect(page.locator(".application-row")).toHaveCount(12);
  await page.getByLabel("地点关键词").fill("西湖&博物馆");
  await page.getByLabel("城市（可选）").fill("杭州");
  const href = await page
    .getByRole("link", { name: "打开高德" })
    .getAttribute("href");
  const url = new URL(href!);
  expect(url.hostname).toBe("uri.amap.com");
  expect(url.searchParams.get("keyword")).toBe("西湖&博物馆");
  expect(url.searchParams.get("city")).toBe("杭州");
  await page.getByRole("button", { name: "办公协作", exact: true }).click();
  await expect(page.locator(".application-row")).toHaveCount(5);
  await page.getByLabel("搜索应用").fill("腾讯");
  await expect(page.locator(".application-row")).toHaveCount(1);
  await page.locator(".application-row").click();
  await expect(
    page.getByRole("heading", { name: "腾讯文档", exact: true }),
  ).toBeVisible();
  await expect(page.getByText(/粘贴网址不会让 Nemi 获得/)).toBeVisible();
  await page.getByLabel("搜索应用").fill("");
  await page.getByRole("button", { name: "全部", exact: true }).click();
  await page.screenshot({
    path: "test-results/nemi-v03-applications.png",
    fullPage: true,
  });
  const title = `验收·日历导出 ${Date.now()}`;
  const origin = process.env.NEMI_BASE_URL!;
  const at = new Date(
    Math.floor((Date.now() + 7 * 86400000) / 60000) * 60000,
  ).toISOString();
  const created = await page.request.post("/api/v1/matters", {
    headers: { Origin: origin, "Idempotency-Key": crypto.randomUUID() },
    data: {
      title,
      source: "仅用于日历导出验收",
      category: "life",
      timezone: "Asia/Shanghai",
      confirmed: true,
      deadline: at,
    },
  });
  expect(created.status()).toBe(201);
  const matter = await created.json();
  await page.reload();
  await page.getByRole("button", { name: "连接应用", exact: true }).click();
  await page
    .locator(".application-row")
    .filter({ hasText: "系统日历" })
    .click();
  await page
    .getByRole("combobox", { name: "选择事项", exact: true })
    .selectOption(matter.id);
  const download = page.waitForEvent("download");
  await page.getByRole("button", { name: "下载日历文件" }).click();
  expect((await download).suggestedFilename()).toBe("nemi-event.ics");
  const content = await page.request.get(
    `/api/v1/matters/${matter.id}/calendar`,
  );
  expect(content.status()).toBe(200);
  expect(await content.text()).toContain(
    `DTSTART:${at.replace(/[-:]/g, "").replace(/\.\d{3}Z$/, "Z")}`,
  );
  const finished = await page.request.patch(`/api/v1/matters/${matter.id}`, {
    headers: { Origin: origin, "Idempotency-Key": crypto.randomUUID() },
    data: { expected_revision: 1, status: "COMPLETED" },
  });
  expect(finished.status()).toBe(200);
});
test("mobile configured channel requires explicit review; ambiguous receipts do not resend", async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  let sends = 0;
  await page.route("**/api/v1/connections", (r) =>
    r.fulfill({
      json: {
        channels: [
          {
            id: "feishu",
            name: "飞书",
            label: "界面测试群（模拟）",
            state: "configured",
          },
          { id: "wecom", name: "企业微信", label: "", state: "unconfigured" },
          { id: "dingtalk", name: "钉钉", label: "", state: "unconfigured" },
        ],
      },
    }),
  );
  await page.route("**/api/v1/connections/feishu/messages", async (r) => {
    sends++;
    expect(r.request().postDataJSON().confirmed).toBe(true);
    expect(r.request().headers()["idempotency-key"]).toBeTruthy();
    await r.fulfill({ json: { status: "UNKNOWN" } });
  });
  await page.getByRole("button", { name: "打开导航" }).click();
  await page.getByRole("button", { name: "连接应用", exact: true }).click();
  await page.locator(".application-row").filter({ hasText: "飞书" }).click();
  await page
    .getByLabel("发送内容", { exact: true })
    .fill("这是一条用于界面验收的消息，不会发送到真实群。");
  await expect(
    page.getByRole("button", { name: "确认发送", exact: true }),
  ).toBeDisabled();
  await page.getByLabel(/我确认将以上内容发送至/).check();
  await page.getByRole("button", { name: "确认发送", exact: true }).click();
  await expect(page.getByText(/发送结果未能确认/)).toBeVisible();
  await expect(
    page.getByRole("button", { name: "确认发送", exact: true }),
  ).toHaveCount(0);
  expect(sends).toBe(1);
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  await page.screenshot({
    path: "test-results/nemi-v03-applications-mobile.png",
    fullPage: true,
  });
});
