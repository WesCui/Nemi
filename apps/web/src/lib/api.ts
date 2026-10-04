export type Item = { text: string; done: boolean };
export type Matter = {
  id: string;
  title: string;
  source: string;
  category: "life" | "travel" | "work";
  status: "ACTIVE" | "COMPLETED";
  revision: number;
  items: Item[];
  deadline: string | null;
  created_at: string;
};
export type Reminder = {
  id: string;
  matter_id: string;
  title: string;
  revision: number;
  nominal_at: string;
  due_at: string;
  quiet: boolean;
  enabled: boolean;
  sync_status: string;
  repeat: "once" | "daily" | "weekdays" | "weekly";
  repeat_until: string | null;
};
export type Run = {
  id: string;
  matter_id: string;
  status: string;
  mode: string;
  result: { summary: string; items: string[] } | null;
  error: string;
  created_at: string;
  used_memory_count: number;
};
export type Notification = {
  id: string;
  title: string;
  matter_id: string;
  status: string;
  created_at: string;
};
export type Dashboard = {
  user: { id: string; name: string };
  matters: Matter[];
  reminders: Reminder[];
  runs: Run[];
  notifications: Notification[];
  model_mode: string;
  memories: Memory[];
  activity: ActivityEvent[];
};
export type Memory = {
  id: string;
  category: "general" | "life" | "travel" | "work";
  text: string;
  revision: number;
  updated_at: string;
};
export type ActivityEvent = {
  sequence: number;
  kind: string;
  subject_id: string;
  created_at: string;
};
export const repeatLabels = {
  once: "仅一次",
  daily: "每天",
  weekdays: "周一至周五",
  weekly: "每周同一天",
};
export function endOfChinaDate(value: string) {
  return isoChina(`${value}T23:59`);
}
export class APIError extends Error {
  constructor(
    message: string,
    public status: number,
  ) {
    super(message);
  }
}
export async function api<T>(
  path: string,
  body?: unknown,
  method = "POST",
  key?: string,
): Promise<T> {
  const response = await fetch(`/api/v1${path}`, {
    method: body === undefined ? "GET" : method,
    credentials: "same-origin",
    cache: "no-store",
    headers:
      body === undefined
        ? {}
        : {
            "Content-Type": "application/json",
            "Idempotency-Key": key || crypto.randomUUID(),
          },
    ...(body === undefined ? {} : { body: JSON.stringify(body) }),
  });
  const data = await response
    .json()
    .catch(() => ({ error: "服务暂时不可用，请稍后重试" }));
  if (!response.ok)
    throw new APIError(data.error || "请求失败", response.status);
  return data as T;
}
export const categories = {
  life: "生活琐事",
  travel: "外出准备",
  work: "工作跟进",
};
export function formatTime(value: string, short = false) {
  return new Intl.DateTimeFormat("zh-CN", {
    timeZone: "Asia/Shanghai",
    month: "long",
    day: "numeric",
    ...(short ? {} : { hour: "2-digit", minute: "2-digit", hour12: false }),
  }).format(new Date(value));
}
export function localInput(value?: string) {
  const t = value ? new Date(value) : new Date(Date.now() + 86400000);
  return new Date(t.getTime() + 8 * 3600000).toISOString().slice(0, 16);
}
export function isoChina(value: string) {
  return new Date(`${value}:00+08:00`).toISOString();
}
export function quietPreview(value: string, quiet: boolean) {
  if (!value) return "";
  const hour = Number(value.slice(11, 13));
  if (!quiet || (hour >= 8 && hour < 22)) return formatTime(isoChina(value));
  const t = new Date(`${value.slice(0, 10)}T08:00:00+08:00`);
  if (hour >= 22) t.setUTCDate(t.getUTCDate() + 1);
  t.setUTCHours(0, 0, 0, 0);
  return `${formatTime(t.toISOString())}（按免打扰时段调整）`;
}
