import { test, expect } from "./authenticated";

test("search, maps and mailbox connections stay in chat and keep credentials out of replies", async ({ page }) => {
  await page.goto("/");
  for (const service of [
    { id: "search", text: "连接联网搜索", field: "博查 API Key" },
    { id: "amap", text: "连接高德地图", field: "Web 服务 Key" },
    { id: "mail", text: "连接邮箱", field: "客户端授权码" },
  ]) {
    await page.getByLabel("告诉妮米你想做的事").fill(service.text);
    await page.getByRole("button", { name: "发送消息", exact: true }).click();
    const card = page.getByLabel("应用连接卡片").last();
    await expect(card.getByLabel(service.field, { exact: true })).toBeVisible();
    await expect(card.getByRole("button", { name: "授权并保存连接" })).toBeDisabled();
    const credential = `fixture-only-${service.id}-credential`;
    await card.getByLabel(service.field, { exact: true }).fill(credential);
    if (service.id === "mail") await card.getByLabel("邮箱地址", { exact: true }).fill("fixture@qq.com");
    await card.getByRole("checkbox").check();
    await card.getByRole("button", { name: "授权并保存连接" }).click();
    await expect(card.getByText("配置已保存", { exact: true })).toBeVisible();
    const state = await (await page.request.get(`/api/v1/connections/services/${service.id}`)).json();
    expect(state.configured).toBe(true); expect(state.verified_at).toBeFalsy();
    const history = await (await page.request.get("/api/v1/conversations")).text();
    expect(history).not.toContain(credential);
    expect(await page.getByLabel("对话内容", { exact: true }).innerText()).not.toContain(credential);
    expect(page.url()).toBe(`${process.env.NEMI_BASE_URL}/`);
  }
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(page.locator(".sidebar")).not.toBeInViewport();
  await page.screenshot({ path: "test-results/nemi-v012-service-connections.png", fullPage: true, animations: "disabled" });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});
