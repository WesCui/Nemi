import { test, expect } from "@playwright/test";

test.beforeEach(async ({ page }, info) => {
  if (info.title.includes("mobile"))
    await page.setViewportSize({ width: 390, height: 844 });
  const code = process.env.NEMI_TEST_INVITE_CODE;
  if (!code) throw new Error("NEMI_TEST_INVITE_CODE required");
  await page.goto("/");
  await page.getByLabel("邀请口令").fill(code);
  await page.getByRole("button", { name: "进入我的空间" }).click();
  await expect(page.getByRole("heading", { name: "今天的安排" })).toBeVisible();
});

test("ongoing matter with confirmed preferences, a bounded schedule and activity", async ({
  page,
}) => {
  const stamp = Date.now();
  const preference = `验收偏好·${stamp}：出行优先高铁，不要把行程排得太赶。`;
  const title = `验收·持续准备出行 ${stamp}`;
  await expect(page.getByRole("button", { name: "退出登录" })).toBeInViewport();
  await page.getByRole("button", { name: "生活偏好", exact: true }).click();
  await page.getByRole("button", { name: "记住一个偏好", exact: true }).click();
  const form = page.getByRole("form", { name: "保存偏好" });
  await form.getByLabel("偏好内容").fill(preference);
  await form.getByLabel("适用场景").selectOption("travel");
  await expect(form.getByRole("button", { name: "保存偏好" })).toBeDisabled();
  await form.getByLabel("我确认保存，用于后续同类清单").check();
  await form.getByRole("button", { name: "保存偏好" }).click();
  const memoryCard = page
    .locator(".memory-card")
    .filter({ hasText: preference });
  await expect(memoryCard).toBeVisible();
  await memoryCard.getByRole("button", { name: "修改", exact: true }).click();
  const changedPreference = `${preference} 清单按出发前的准备顺序排列。`;
  await form.getByLabel("偏好内容").fill(changedPreference);
  await form.getByLabel("我确认保存，用于后续同类清单").check();
  await form.getByRole("button", { name: "保存偏好" }).click();
  await expect(
    page.getByText(changedPreference, { exact: true }),
  ).toBeVisible();
  await page.screenshot({
    path: "test-results/nemi-v02-memory.png",
    fullPage: true,
  });
  await page
    .getByRole("button", { name: "记一件事", exact: true })
    .first()
    .click();
  const create = page.getByRole("dialog", { name: "记一件事" });
  await create.getByLabel("事项名称").fill(title);
  await create
    .getByLabel("补充资料")
    .fill("杭州出行，预算1500元，先整理待确认项目。");
  await create.getByLabel("事项类型").selectOption("travel");
  await create.getByLabel("设置提醒", { exact: true }).check();
  await create.getByLabel("重复频率").selectOption("weekly");
  await expect(create.getByLabel("结束日期 · 北京时间")).toHaveValue(
    /\d{4}-\d{2}-\d{2}/,
  );
  await expect(create.getByLabel("参考已确认的偏好")).toBeChecked();
  await create.getByLabel("我已确认事项内容与时间（北京时间）").check();
  await create.getByRole("button", { name: "确认并保存" }).click();
  const detail = page.getByRole("dialog", { name: "事项详情" });
  await expect(
    detail.getByText(/本次整理参考了 [1-8] 条已确认偏好/),
  ).toBeVisible();
  await expect(detail.getByText(/每周同一天/)).toBeVisible();
  await expect(detail.getByText(/结束日期：/)).toBeVisible();
  await detail.getByRole("button", { name: "补充或修改资料" }).click();
  await detail
    .getByLabel("更新资料")
    .fill("杭州出行，预算改为1000元，同行增加一人。这是最新确认的信息。");
  await detail.getByRole("button", { name: "保存资料" }).click();
  await expect(
    detail.getByText(
      "杭州出行，预算改为1000元，同行增加一人。这是最新确认的信息。",
      { exact: true },
    ),
  ).toBeVisible();
  await detail.getByLabel("参考已确认的偏好").uncheck();
  await detail.getByRole("button", { name: "重新整理清单" }).click();
  await expect
    .poll(async () => {
      const d = await (await page.request.get("/api/v1/dashboard")).json();
      const m = d.matters.find(
        (matter: { title: string }) => matter.title === title,
      );
      const run = d.runs.find(
        (r: { matter_id: string }) => r.matter_id === m.id,
      );
      return { status: run.status, memories: run.used_memory_count };
    })
    .toEqual({ status: "SUCCEEDED", memories: 0 });
  await detail.getByRole("button", { name: "设置", exact: true }).click();
  await detail.getByLabel("启用此提醒").uncheck();
  await detail.getByLabel("确认此次更改").check();
  await detail.getByRole("button", { name: "保存提醒" }).click();
  await expect(detail.getByText("暂未设置提醒", { exact: true })).toBeVisible();
  await detail.getByRole("button", { name: "标记已完成", exact: true }).click();
  await expect(detail.locator(".status-badge")).toHaveText("已完成");
  await detail
    .getByRole("button", { name: "关闭", exact: true })
    .last()
    .click();
  await page.getByRole("button", { name: "工作动态", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "妮米的工作动态" }),
  ).toBeVisible();
  await expect(
    page
      .locator(".activity-timeline")
      .getByText("清单已生成", { exact: true })
      .first(),
  ).toBeVisible();
  await page.screenshot({
    path: "test-results/nemi-v02-activity.png",
    fullPage: true,
  });
  await page.getByRole("button", { name: "生活偏好", exact: true }).click();
  const updatedCard = page
    .locator(".memory-card")
    .filter({ hasText: changedPreference });
  await updatedCard.getByRole("button", { name: "移除", exact: true }).click();
  await updatedCard
    .getByRole("button", { name: "确认移除", exact: true })
    .click();
  await expect(page.getByText(changedPreference, { exact: true })).toHaveCount(
    0,
  );
  await page.reload();
  await page.getByRole("button", { name: "生活偏好", exact: true }).click();
  await expect(page.getByText(changedPreference, { exact: true })).toHaveCount(
    0,
  );
});

test("mobile preference and activity surfaces stay usable", async ({
  page,
}) => {
  await page.getByRole("button", { name: "打开导航" }).click();
  await expect(page.getByRole("button", { name: "退出登录" })).toBeInViewport();
  await page.getByRole("button", { name: "生活偏好", exact: true }).click();
  await expect
    .poll(() =>
      page
        .locator(".sidebar")
        .evaluate((element) => element.getBoundingClientRect().right),
    )
    .toBeLessThanOrEqual(1);
  await page.getByRole("button", { name: "记住一个偏好", exact: true }).click();
  await expect(page.getByRole("form", { name: "保存偏好" })).toBeVisible();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
  await page.screenshot({
    path: "test-results/nemi-v02-memory-mobile.png",
    fullPage: true,
  });
  await page.getByRole("button", { name: "打开导航" }).click();
  await page.getByRole("button", { name: "工作动态", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "妮米的工作动态" }),
  ).toBeVisible();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
});
