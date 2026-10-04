"use client";

import { useEffect, useRef, useState } from "react";
import { api } from "@/lib/api";
import { BotSettings } from "@/components/bot-settings";
import { FeishuDocuments } from "@/components/feishu-documents";

export type AppAction = { id: string; kind: string; status: string; dispatch_status?: string; payload: { title: string; app_id?: string; channel_id?: string; recipient_label?: string; text?: string } };
type Channel = { id: string; name: string; label: string; state: string; revision?: number; verified_at?: string | null };

function ConnectionCard({ action, onChanged, onContinue, canContinue, showContinue }: { action: AppAction; onChanged: () => Promise<void>; onContinue: () => Promise<void>; canContinue: boolean; showContinue: boolean }) {
  const [channel, setChannel] = useState<Channel | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const key = useRef(crypto.randomUUID());
  const id = action.payload.app_id || "";
  async function load() {
    const data = await api<{ channels: Channel[] }>("/connections");
    const current = data.channels.find((c) => c.id === id) || null;
    setChannel(current); return current;
  }
  useEffect(() => {
    if (id === "feishu_documents") return;
    let disposed = false;
    api<{ channels: Channel[] }>("/connections").then((data) => { if (!disposed) setChannel(data.channels.find((c) => c.id === id) || null); }).catch((e: Error) => { if (!disposed) setError(e.message); });
    return () => { disposed = true; };
  }, [id]);
  async function complete() {
    setBusy(true); setError("");
    try {
      await api(`/agent/actions/${action.id}/approve`, { confirmed: true }, "POST", key.current);
      await onChanged();
    } catch (e) { setError((e as Error).message); }
    finally { setBusy(false); }
  }
  return <aside className="agent-action agent-connection" aria-label="应用连接卡片">
    <span className="section-kicker">{action.status === "APPROVED" ? "配置已保存" : action.status === "DECLINED" ? "已取消连接引导" : "在这里连接应用"}</span>
    <h3>{action.payload.title}</h3>
    {action.status === "PENDING" && (id === "feishu_documents" ? <FeishuDocuments setupOnly onConnected={complete} /> : channel ? <>
      <p>请让群管理员提供机器人的连接地址，填写在下面。连接后，告诉妮米要发送的内容；每条消息都由你确认接收群和正文。</p>
      <BotSettings key={channel.revision || 0} channel={channel} onChanged={async () => { const c = await load(); if (c?.state === "configured") await complete(); }} />
      {channel.state === "configured" && <button type="button" className="primary" disabled={busy} onClick={() => void complete()}>完成连接，继续对话</button>}
    </> : <p>正在读取连接信息…</p>)}
    {action.status === "APPROVED" && <><p>配置保存不代表平台权限已验证；实际结果会显示在对话中。</p>{showContinue && <button type="button" className="primary" disabled={!canContinue} onClick={() => void onContinue()}>继续刚才的任务</button>}</>}
    {error && <p className="form-error" role="alert">{error}</p>}
    <small>凭据只在专用字段中加密保存，请勿发到聊天里。首次开通平台权限仍需你或管理员授权。</small>
  </aside>;
}

function MessageCard({ action, onChanged }: { action: AppAction; onChanged: () => Promise<void> }) {
  const [busy, setBusy] = useState(false), [error, setError] = useState("");
  const keys = useRef<Record<string, string>>({});
  const sending = useRef(false);
  async function decide(decision: string) {
    if (sending.current) return;
    sending.current = true; setBusy(true); setError("");
    try {
      keys.current[decision] ||= crypto.randomUUID();
      await api(`/agent/actions/${action.id}/${decision}`, { confirmed: true }, "POST", keys.current[decision]);
      await onChanged();
    } catch (e) { setError((e as Error).message); }
    finally { sending.current = false; setBusy(false); }
  }
  return <aside className="agent-action" aria-label="群消息提案">
    <span className="section-kicker">{action.status === "PENDING" ? "发送前请核对" : action.status === "DECLINED" ? "已取消发送" : "发送回执"}</span>
    <h3>{action.payload.title}</h3>
    <p className="agent-recipient">接收群：{action.payload.recipient_label} · 群内成员可见</p>
    <div className="message-preview"><p>{action.payload.text}</p></div>
    {action.status === "PENDING" && <div className="agent-action-buttons"><button type="button" className="primary" disabled={busy} onClick={() => void decide("approve")}>{busy ? "处理中…" : "确认发送"}</button><button type="button" className="text-button" disabled={busy} onClick={() => void decide("dismiss")}>取消发送</button></div>}
    {action.status === "APPROVED" && <p role="status">{action.dispatch_status === "DELIVERED" ? "平台已接受消息，请在接收群核对。" : action.dispatch_status === "REJECTED" ? "平台拒绝了消息，请检查连接设置。" : "发送结果暂时无法确认，请先在群里核对；妮米不会自动重发。"}</p>}
    {error && <p className="form-error" role="alert">{error}</p>}
  </aside>;
}

export function AgentAppAction({ action, onChanged, onContinue, canContinue, showContinue = true }: { action: AppAction; onChanged: () => Promise<void>; onContinue: () => Promise<void>; canContinue: boolean; showContinue?: boolean }) {
  return action.kind === "connect_app" ? <ConnectionCard action={action} onChanged={onChanged} onContinue={onContinue} canContinue={canContinue} showContinue={showContinue} /> : <MessageCard action={action} onChanged={onChanged} />;
}
