"use client";

import { useRef, useState } from "react";
import { api } from "@/lib/api";

export type Continuation = { run_id: string; goal: string; next_step: string; status: string; authorized: boolean; next_run_id: string; error: string; expires_at: string };
const errors: Record<string, string> = { MODEL_CONFIG_REVOKED: "原来的模型已移除，请配置模型并重新交代任务。", NEW_USER_MESSAGE: "新的消息已改变对话方向，旧任务不再自动继续。", AGENT_CANCELLED: "你已停止后续执行。", CHAT_QUEUE_FULL: "等待任务较多，请稍后重新交代任务。", CHAT_TURN_LIMIT: "这段对话已达到轮次上限，请新开对话。", CHAT_CONTEXT_TOO_LARGE: "背景资料过长，请新开对话并提供关键要求。", CONTINUATION_LIMIT: "本次连续执行已达到阶段上限，请重新交代目标。" };
export function AgentContinuation({ value, model, onChanged }: { value: Continuation; model: string; onChanged: () => Promise<void> }) {
  const [busy, setBusy] = useState(false), [error, setError] = useState("");
  const keys = useRef<Record<string, string>>({});
  const expired = value.status === "EXPIRED" || (value.status === "WAITING" && Date.parse(value.expires_at) <= Date.now());
  const waiting = value.status === "WAITING" && !expired;
  const title = expired ? "这次等待已到期" : value.status === "STARTED" ? "已继续执行" : waiting ? value.authorized ? "确认齐全后，妮米会继续" : "确认之后，接着做下一步" : value.status === "BLOCKED" ? "后续执行暂时停止" : "已停止后续执行";
  async function decide(decision: "start" | "stop") {
    if (busy) return; setBusy(true); setError("");
    try { keys.current[decision] ||= crypto.randomUUID(); await api(`/chat/continuations/${value.run_id}/${decision}`, { confirmed: true }, "POST", keys.current[decision]); await onChanged(); }
    catch (e) { setError((e as Error).message); } finally { setBusy(false); }
  }
  return <aside className="agent-continuation" aria-label="确认后继续任务">
    <span className="section-kicker">{title}</span>
    <h3>{value.goal}</h3><p>{value.next_step}</p>
    {waiting && <small>{value.authorized ? "请核对并确认或取消上方操作。可以关闭页面，后台会保留等待。" : `继续使用本轮的「${model}」，会产生模型费用并计入现有预算。允许继续不会替你确认上方操作。`}</small>}
    {expired && <p>等待已到期，请重新交代任务和日期。</p>}
    {!waiting && !expired && value.error && <p>{errors[value.error] || "本轮未继续，请核对上方任务结果后重新交代目标。"}</p>}
    {error && <p className="form-error" role="alert">{error}</p>}
    {waiting && <div className="agent-action-buttons">{!value.authorized && <button type="button" className="primary" disabled={busy} onClick={() => void decide("start")}>{busy ? "处理中…" : "允许确认后继续"}</button>}<button type="button" className="text-button" disabled={busy} onClick={() => void decide("stop")}>停止后续执行</button></div>}
  </aside>;
}
