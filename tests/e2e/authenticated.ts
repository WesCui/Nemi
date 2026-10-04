import { test as base, expect, type BrowserContext } from "@playwright/test";

type State = Awaited<ReturnType<BrowserContext["storageState"]>>;

// Exercise the real login UI once per worker, then isolate browser contexts
// while sharing that authenticated session. The product login limit stays intact.
export const test = base.extend<{}, { authenticatedState: State }>({
  authenticatedState: [async ({ browser }, use) => {
    const code = process.env.NEMI_TEST_INVITE_CODE;
    if (!code) throw new Error("NEMI_TEST_INVITE_CODE must be configured");
    const context = await browser.newContext({ baseURL: process.env.NEMI_BASE_URL || "http://localhost:3000" });
    let state: State;
    try {
      const page = await context.newPage();
      await page.goto("/");
      await page.getByLabel("邀请口令").fill(code);
      await page.getByRole("button", { name: "进入我的空间" }).click();
      await expect(page.getByRole("heading", { name: "有什么想交给妮米？" })).toBeVisible();
      state = await context.storageState();
    } finally {
      await context.close();
    }
    await use(state);
  }, { scope: "worker" }],
  storageState: async ({ authenticatedState }, use) => { await use(authenticatedState); },
});
export { expect };
