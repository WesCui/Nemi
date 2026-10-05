import { type Page } from "@playwright/test";
import { test, expect } from "./authenticated";

test.beforeEach(async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "有什么想交给妮米？" })).toBeVisible();
  await page.getByRole("button", { name: "新对话", exact: true }).click();
});
async function begin(page: Page, title: string) {
  await page.getByLabel("告诉妮米你想做的事").fill(`先保存事项再整理报告：${title}`);
  await page.getByRole("button", { name: "发送消息", exact: true }).click();
  const wait = page.getByLabel("确认后继续任务", { exact: true });
  await expect(wait.getByRole("button", { name: "允许确认后继续" })).toBeVisible();
  await expect(page.getByRole("button", { name: "停止本次任务", exact: true })).toHaveCount(0);
  return wait;
}
async function detail(page: Page) {
  const list = await (await page.request.get("/api/v1/conversations")).json();
  return (await page.request.get(`/api/v1/conversations/${list.conversations[0].id}`)).json();
}
test("approval resumes in the background once, preserves the model and delivers a file", async ({ page }) => {
  let userSubmissions = 0;
  page.on("request", r => { if (r.method() === "POST" && r.url().endsWith("/api/v1/chat/messages")) userSubmissions++; });
  const title = `确认后交付 ${Date.now()}`;
  let wait = await begin(page, title);
  const before = await detail(page);
  expect(before.turns).toHaveLength(1); expect(before.turns[0].continuation.authorized).toBe(false);
  await wait.getByRole("button", { name: "允许确认后继续" }).click();
  await expect(wait.getByText("确认齐全后，妮米会继续", { exact: true })).toBeVisible();
  await page.reload(); wait = page.getByLabel("确认后继续任务", { exact: true });
  await expect(wait.getByText(/可以关闭页面/)).toBeVisible();
  await page.screenshot({ path: "test-results/nemi-v010-waiting.png", fullPage: true, animations: "disabled" });
  await page.setViewportSize({ width: 390, height: 844 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await page.screenshot({ path: "test-results/nemi-v010-waiting-mobile.png", fullPage: true, animations: "disabled" });
  await page.getByLabel("事项提案", { exact: true }).getByRole("button", { name: "确认创建" }).click();
  await expect(page.getByText("已核对保存结果，并生成文字报告。", { exact: true })).toBeVisible();
  const preview = page.getByRole("button", { name: `预览 ${title}.md`, exact: true });
  await expect(preview).toBeVisible();
  await preview.click();
  const report = page.getByRole("dialog", { name: `${title}.md资料预览`, exact: true });
  await expect(report.locator(".file-text")).toContainText("已保存状态：ACTIVE");
  await report.getByRole("button", { name: "关闭资料预览" }).click();
  await expect(page.getByRole("link", { name: `下载 ${title}.md`, exact: true })).toBeVisible();
  const after = await detail(page);
  expect(after.turns).toHaveLength(2); expect(after.turns[1].origin).toBe("continuation");
  expect(after.turns[1].model).toBe(after.turns[0].model);
  expect(after.turns[0].continuation.next_run_id).toBe(after.turns[1].run_id);
  expect(after.turns[1].files).toHaveLength(1); expect(userSubmissions).toBe(1);
  await page.reload();
  await expect(page.getByText("确认结束，妮米继续推进任务", { exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: `预览 ${title}.md`, exact: true })).toBeVisible();
  await page.screenshot({ path: "test-results/nemi-v010-resumed-mobile.png", fullPage: true, animations: "disabled" });
});
test("stopping continuation preserves a separately confirmed operation without running follow-up", async ({ page }) => {
  const title = `只保存不续接 ${Date.now()}`;
  const wait = await begin(page, title);
  await wait.getByRole("button", { name: "允许确认后继续" }).click();
  await wait.getByRole("button", { name: "停止后续执行" }).click();
  await expect(wait.getByText("已停止后续执行", { exact: true })).toBeVisible();
  await page.getByLabel("事项提案", { exact: true }).getByRole("button", { name: "确认创建" }).click();
  await expect(page.getByLabel("事项提案", { exact: true }).getByText("已确认并保存", { exact: true })).toBeVisible();
  await page.reload(); const current = await detail(page);
  expect(current.turns).toHaveLength(1); expect(current.turns[0].continuation.status).toBe("CANCELLED");
  const dashboard = await (await page.request.get("/api/v1/dashboard")).json();
  expect(dashboard.matters.filter((m: { title: string }) => m.title === title)).toHaveLength(1);
});
test("a new user message supersedes an old wait and cannot be overwritten by late approval", async ({ page }) => {
  const title = `改变目标 ${Date.now()}`;
  const wait = await begin(page, title);
  await wait.getByRole("button", { name: "允许确认后继续" }).click();
  await page.getByLabel("告诉妮米你想做的事").fill("改为先聊别的问题");
  await page.getByRole("button", { name: "发送消息", exact: true }).click();
  await expect(page.getByLabel("对话内容", { exact: true }).locator(".assistant .chat-reply").last()).toContainText("改为先聊别的问题");
  await expect(wait.getByText(/新的消息已改变对话方向/)).toBeVisible();
  await page.getByLabel("事项提案", { exact: true }).getByRole("button", { name: "确认创建" }).click();
  await page.reload(); const current = await detail(page);
  expect(current.turns).toHaveLength(2); expect(current.turns.every((t: { origin: string }) => t.origin === "user")).toBe(true);
  expect(current.turns[0].continuation.error).toBe("NEW_USER_MESSAGE");
});
