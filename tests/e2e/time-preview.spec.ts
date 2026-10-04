import { test, expect } from "@playwright/test";
import { isoChina, quietPreview } from "../../apps/web/src/lib/api";

test("quiet preview uses the confirmed China calendar date across UTC boundaries", () => {
  expect(isoChina("2026-10-05T07:00")).toBe("2026-10-04T23:00:00.000Z");
  expect(quietPreview("2026-10-05T07:00", true)).toContain("10月5日 08:00");
  expect(quietPreview("2026-12-31T23:30", true)).toContain("1月1日 08:00");
  expect(quietPreview("2026-10-05T08:00", true)).toBe("10月5日 08:00");
  expect(quietPreview("2026-10-05T07:00", false)).toBe("10月5日 07:00");
});
