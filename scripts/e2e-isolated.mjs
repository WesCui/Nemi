import { spawn, execFileSync } from "node:child_process";
import { randomUUID, randomBytes } from "node:crypto";
import { fileURLToPath } from "node:url";
import path from "node:path";
import { mkdirSync } from "node:fs";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const testBin = path.join(root, ".cache/bin/e2e");
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
  MODEL_PROVIDER: "qwen",
  MODEL_NAME: "contract-fixture",
  MODEL_INPUT_PRICE_MICRO_CNY: "2000000",
  MODEL_OUTPUT_PRICE_MICRO_CNY: "4000000",
  NEMI_E2E_WORKER: "1",
  API_ORIGIN: "http://127.0.0.1:18080",
  NEMI_BASE_URL: "http://localhost:3310",
  NEMI_WEB_DIST_DIR: ".next-e2e",
  NEXT_TELEMETRY_DISABLED: "1",
};
env.NEMI_TEST_INVITE_CODE = env.APP_INVITE_CODE;
const suffix = randomUUID().slice(0, 8);
env.APP_BOOTSTRAP_ID = `nemi-e2e-${suffix}`;
env.APP_FILES_ROOT = path.join(root,`.cache/e2e-${suffix}/files`);
for (const key of Object.keys(env)) if (key.startsWith("FILES_S3_")) delete env[key];
env.APP_OUTBOX_WORKSPACE = env.APP_BOOTSTRAP_ID;
env.TEMPORAL_RUN_QUEUE = `nemi-e2e-${suffix}-runtime`;
env.TEMPORAL_REMINDER_QUEUE = `nemi-e2e-${suffix}-reminders`;
for (const key of Object.keys(env))
  if (key.startsWith("CONNECTOR_") || key === "MODEL_API_KEY") delete env[key];
env.MODEL_API_KEY = "fixture-not-a-real-provider-key";
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
  // All test services must share the current contracts; never use stale product
  // binaries or overwrite the binaries of the user's running local services.
  mkdirSync(testBin, { recursive: true });
  for (const name of ["control-api", "notification-worker", "relay", "file-parser"])
    execFileSync("go", ["build", "-o", path.join(testBin, name + (process.platform === "win32" ? ".exe" : "")), `./cmd/${name}`], { cwd: root, env, stdio: ["ignore", "ignore", "pipe"], windowsHide: true });
  for (const name of [
    "control-api",
    "notification-worker",
    "relay",
  ])
    start(
      path.join(
        testBin,
        name + (process.platform === "win32" ? ".exe" : ""),
      ),
      [],
    );
  execFileSync("go", ["test", "-c", "-o", path.join(testBin, "e2e-runtime.test" + (process.platform === "win32" ? ".exe" : "")), "./internal/runtime"], { cwd: root, env, stdio: ["ignore", "ignore", "pipe"], windowsHide: true });
  start(path.join(testBin, "e2e-runtime.test" + (process.platform === "win32" ? ".exe" : "")), ["-test.run=^TestE2EWorkerService$", "-test.timeout=15m"]);
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
