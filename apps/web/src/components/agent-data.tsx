"use client";

import { useRef, useState } from "react";
import { api } from "@/lib/api";

type Item = { text: string; done: boolean };
type Matter = { title: string; source: string; status: string; deadline?: string; items: Item[] };
type Reminder = { enabled: boolean; nominal_at: string; due_at: string; quiet: boolean; repeat: string; repeat_until?: string };
type Payload = {
  title: string; before_matter?: Matter; before_reminder?: Reminder; before_memory?: { text: string; category: string };
  matter_patch?: Partial<Matter> & { clear_deadline?: boolean };
  reminder_patch?: { enabled: boolean; at: string; quiet: boolean; repeat: string; repeat_until?: string };
  memory_patch?: { text: string; category: string };
};
const statuses: Record<string, string> = { ACTIVE: "继续跟进", COMPLETED: "已完成", ARCHIVED: "已归档" };
const categories: Record<string, string> = { general: "通用", life: "生活", travel: "出行", work: "工作" };
const repeats: Record<string, string> = { once: "一次", daily: "每天", weekdays: "周一至周五", weekly: "每周" };
function date(value?: string) { return value ? `${new Date(value).toLocaleString("zh-CN", { timeZone: "Asia/Shanghai", hour12: false })} 北京时间` : "未设置"; }
function reminder(r?: Reminder | Payload["reminder_patch"]) {
  if (!r) return "未设置";
  if (!r.enabled) return "已停用";
  const at = "nominal_at" in r ? r.nominal_at : r.at;
  return `${date(at)} · ${repeats[r.repeat] || r.repeat}${r.repeat_until ? `，截至 ${date(r.repeat_until)}` : ""}${r.quiet ? " · 22:00–08:00 延至早上08:00" : ""}`;
}
export function isDataProposal(kind: string) { return ["update_matter", "update_reminder", "save_memory", "delete_memory"].includes(kind); }
export function AgentDataAction({ action, onChanged }: { action: { id: string; kind: string; status: string; payload: unknown }; onChanged: () => Promise<void> }) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const keys = useRef<Record<string, string>>({});
  const p = action.payload as Payload;
  const fields: { label: string; before: string; after: string }[] = [];
  const patch = p.matter_patch;
  if (patch && p.before_matter) {
    const before = p.before_matter;
    if (patch.title !== undefined) fields.push({ label: "标题", before: before.title, after: patch.title });
    if (patch.source !== undefined) fields.push({ label: "资料", before: before.source || "未填写", after: patch.source || "清空资料" });
    if (patch.status !== undefined) fields.push({ label: "状态", before: statuses[before.status] || before.status, after: statuses[patch.status] || patch.status });
    if (patch.deadline || patch.clear_deadline) fields.push({ label: "截止时间", before: date(before.deadline), after: patch.clear_deadline ? "移除截止时间" : date(patch.deadline) });
    if (patch.items !== undefined) fields.push({ label: "准备清单", before: (before.items || []).map((i) => `${i.done ? "✓" : "○"} ${i.text}`).join("\n") || "无清单", after: patch.items.map((i) => `${i.done ? "✓" : "○"} ${i.text}`).join("\n") || "清空清单" });
    if (["COMPLETED", "ARCHIVED"].includes(patch.status || "") && p.before_reminder?.enabled) fields.push({ label: "站内提醒", before: reminder(p.before_reminder), after: "同时停用" });
  }
  if (p.reminder_patch) fields.push({ label: "站内提醒", before: reminder(p.before_reminder), after: reminder(p.reminder_patch) });
  if (["save_memory", "delete_memory"].includes(action.kind)) {
    fields.push({ label: "偏好内容", before: p.before_memory?.text || "尚未保存", after: action.kind === "delete_memory" ? "移除这条保存的偏好" : p.memory_patch?.text || "" });
    if (p.memory_patch) fields.push({ label: "适用场景", before: p.before_memory ? categories[p.before_memory.category] : "未设置", after: categories[p.memory_patch.category] || p.memory_patch.category });
  }
  async function decide(decision: string) {
    if (busy) return;
    setBusy(true); setError("");
    try { keys.current[decision] ||= crypto.randomUUID(); await api(`/agent/actions/${action.id}/${decision}`, { confirmed: true }, "POST", keys.current[decision]); await onChanged(); }
    catch (e) { setError((e as Error).message); } finally { setBusy(false); }
  }
  return <aside className="agent-action agent-data" aria-label="数据修改提案">
    <span className="section-kicker">{action.status === "PENDING" ? "等待你确认" : action.status === "APPROVED" ? "已确认并保存" : "已取消"}</span>
    <h3>{p.title}</h3>
    <div className="agent-diff-head"><span>当前内容</span><span>准备修改为</span></div>
    {fields.map((f) => <section className="agent-diff" key={f.label}><h4>{f.label}</h4><div><p>{f.before}</p><p>{f.after}</p></div></section>)}
    {patch?.status === "ARCHIVED" && <p className="agent-data-note">归档后从日常列表移除，历史和资料保留，可以请妮米恢复。</p>}
    {patch?.status === "ACTIVE" && <p className="agent-data-note">重新跟进不会自动恢复旧提醒，需要单独确认新的提醒时间。</p>}
    {action.kind === "delete_memory" && <p className="agent-data-note">只移除保存的偏好，不删除聊天原文。</p>}
    {p.reminder_patch && <p className="agent-data-note">提醒仅在妮米站内可见；免打扰可能调整实际提醒时间。</p>}
    {error && <p className="form-error" role="alert">{error}</p>}
    {action.status === "PENDING" && <div className="agent-action-buttons"><button type="button" className="primary" disabled={busy} onClick={() => void decide("approve")}>{busy ? "处理中…" : "确认修改"}</button><button type="button" className="text-button" disabled={busy} onClick={() => void decide("dismiss")}>取消提案</button></div>}
  </aside>;
}
