import { spawn, execFileSync } from "node:child_process";
import { randomUUID, randomBytes } from "node:crypto";
import { fileURLToPath } from "node:url";
import path from "node:path";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const database = process.env.TEST_DATABASE_URL;
if (!database || new URL(database).pathname !== "/nemi_test")
  throw new Error("Configure the dedicated nemi_test database first.");
const env = {
  ...process.env,
  APP_ENV: "development",
  APP_CREDENTIAL_KEY: randomBytes(32).toString("hex"),
  DATABASE_URL: database,
  TEMPORAL_ADDRESS: process.env.TEST_TEMPORAL_ADDRESS || "127.0.0.1:7233",
  APP_INVITE_CODE: randomUUID(),
  APP_LISTEN: "127.0.0.1:18080",
  APP_ORIGIN: "http://localhost:3310",
  MODEL_PROVIDER: "demo",
  API_ORIGIN: "http://127.0.0.1:18080",
  NEMI_BASE_URL: "http://localhost:3310",
  NEMI_WEB_DIST_DIR: ".next-e2e",
  NEXT_TELEMETRY_DISABLED: "1",
};
env.NEMI_TEST_INVITE_CODE = env.APP_INVITE_CODE;
const suffix = randomUUID().slice(0, 8);
env.APP_BOOTSTRAP_ID = `nemi-e2e-${suffix}`;
env.APP_OUTBOX_WORKSPACE = env.APP_BOOTSTRAP_ID;
env.TEMPORAL_RUN_QUEUE = `nemi-e2e-${suffix}-runtime`;
env.TEMPORAL_REMINDER_QUEUE = `nemi-e2e-${suffix}-reminders`;
for (const key of Object.keys(env))
  if (key.startsWith("CONNECTOR_") || key === "MODEL_API_KEY") delete env[key];
const processes = [];
function start(command, args, cwd = root) {
  const child = spawn(command, args, {
    cwd,
    env,
    stdio: ["ignore", "pipe", "pipe"],
    windowsHide: true,
  });
  child.on("error", () =>
    console.error("Unable to start an isolated test service."),
  );
  child.stdout.on("data", () => {});
  child.stderr.on("data", () => {});
  processes.push(child);
  return child;
}
async function ready(url) {
  for (let i = 0; i < 60; i++) {
    if (processes.some((c) => c.exitCode !== null))
      throw new Error("An isolated service exited during startup.");
    try {
      if ((await fetch(url)).ok) return;
    } catch {}
    await new Promise((r) => setTimeout(r, 500));
  }
  throw new Error("Isolated test services did not become ready.");
}
function cleanup() {
  for (const child of processes.reverse()) {
    if (!child.pid || child.exitCode !== null) continue;
    if (process.platform === "win32") {
      try {
        execFileSync("taskkill.exe", ["/PID", String(child.pid), "/T", "/F"], {
          stdio: "ignore",
          windowsHide: true,
        });
      } catch {}
    } else child.kill("SIGTERM");
  }
}
process.once("SIGINT", () => {
  cleanup();
  process.exit(130);
});
process.once("SIGTERM", () => {
  cleanup();
  process.exit(143);
});
try {
  // Never target an already running app on either test port.
  for (const port of [18080, 3310]) {
    try {
      await fetch(`http://127.0.0.1:${port}`, {
        signal: AbortSignal.timeout(800),
      });
      throw new Error("An isolated test port is occupied.");
    } catch (e) {
      if (e.message === "An isolated test port is occupied.") throw e;
    }
  }
  for (const name of [
    "control-api",
    "runtime-worker",
    "notification-worker",
    "relay",
  ])
    start(
      path.join(
        root,
        ".cache",
        "bin",
        name + (process.platform === "win32" ? ".exe" : ""),
      ),
      [],
    );
  await ready("http://127.0.0.1:18080/healthz");
  start(
    process.execPath,
    [
      path.join(root, "apps/web/node_modules/next/dist/bin/next"),
      "dev",
      "--hostname",
      "127.0.0.1",
      "--port",
      "3310",
    ],
    path.join(root, "apps/web"),
  );
  await ready("http://localhost:3310");
  const test = spawn(
    process.execPath,
    [
      path.join(root, "node_modules/@playwright/test/cli.js"),
      "test",
      ...process.argv.slice(2),
    ],
    { cwd: root, env, stdio: "inherit", windowsHide: true },
  );
  processes.push(test);
  process.exitCode = await new Promise((resolve) => {
    test.on("exit", (code) => resolve(code ?? 1));
    test.on("error", () => resolve(1));
  });
} finally {
  cleanup();
}
