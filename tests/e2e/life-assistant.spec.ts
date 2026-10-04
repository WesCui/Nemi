import { test, expect } from "@playwright/test";

const code = process.env.NEMI_TEST_INVITE_CODE;
test.beforeEach(async ({ page }, info) => {
  if (info.title.includes("mobile")) {
    await page.setViewportSize({ width: 390, height: 844 });
  }
  if (!code) throw new Error("NEMI_TEST_INVITE_CODE must be configured");
  await page.goto("/");
  await page.getByLabel("邀请口令").fill(code);
  await page.getByRole("button", { name: "进入我的空间" }).click();
  await expect(page.getByRole("heading", { name: "今天的安排" })).toBeVisible();
  const mode = await page.request.get("/api/v1/me");
  expect((await mode.json()).model_mode).toBe("demo");
});

test("confirmed matter, durable plan, revisioned reminder, checklist and completion", async ({
  page,
}) => {
  const errors: string[] = [];
  page.on("pageerror", (e) => errors.push(e.message));
  await page.screenshot({
    path: "test-results/nemi-desktop.png",
    fullPage: true,
  });
  const title = `验收·周末杭州之行 ${Date.now()}`;
  await page
    .getByRole("button", { name: "记一件事", exact: true })
    .first()
    .click();
  const create = page.getByRole("dialog", { name: "记一件事" });
  await create.getByLabel("事项名称").fill(title);
  await create
    .getByLabel("补充资料")
    .fill(
      "上海出发，周末去杭州，两位成人，预算1500元。开放时间和预约要求尚未核验。",
    );
  await create.getByLabel("事项类型").selectOption("travel");
  await create.getByLabel("设置提醒", { exact: true }).check();
  await expect(
    create.getByRole("button", { name: "确认并保存" }),
  ).toBeDisabled();
  await create.getByLabel("我已确认事项内容与时间（北京时间）").check();
  await create.getByRole("button", { name: "确认并保存" }).click();
  const detail = page.getByRole("dialog", { name: "事项详情" });
  await expect(detail.getByRole("heading", { name: title })).toBeVisible();
  await expect(
    detail.getByText("清单已生成 · 本地演示生成", { exact: true }),
  ).toBeVisible();
  await expect(
    detail.getByText("确认出发时间、同行人数与预算", { exact: true }),
  ).toBeVisible();
  await detail.locator(".checklist-item").first().getByRole("checkbox").check();
  await expect(detail.locator(".checklist-item.done")).toHaveCount(1);
  await detail.getByRole("button", { name: "设置", exact: true }).click();
  await detail.getByLabel("启用此提醒").uncheck();
  await detail.getByLabel("确认此次更改").check();
  await detail.getByRole("button", { name: "保存提醒" }).click();
  await expect(detail.getByText("暂未设置提醒", { exact: true })).toBeVisible();
  const download = page.waitForEvent("download");
  await detail.getByRole("button", { name: "下载清单" }).click();
  expect((await download).suggestedFilename()).toBe("nemi-checklist.md");
  await detail
    .getByRole("button", { name: "关闭", exact: true })
    .last()
    .click();
  await page.reload();
  await expect(page.getByRole("heading", { name: title })).toBeVisible();
  await page.getByRole("heading", { name: title }).click();
  await expect(
    page.getByRole("dialog").locator(".checklist-item.done"),
  ).toHaveCount(1);
  await page.getByRole("button", { name: "标记已完成", exact: true }).click();
  await expect(page.getByRole("dialog").locator(".status-badge")).toHaveText(
    "已完成",
  );
  expect(errors).toEqual([]);
});

test("mobile layout and honest connector states", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
  await page.screenshot({
    path: "test-results/nemi-mobile.png",
    fullPage: true,
  });
  await page.getByRole("button", { name: "打开导航" }).click();
  await page.getByRole("button", { name: "连接应用", exact: true }).click();
  await expect(page.getByText("待配置", { exact: true })).toHaveCount(3);
  await expect(
    page.getByRole("heading", { name: "高德地图", exact: true }),
  ).toBeVisible();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
});
