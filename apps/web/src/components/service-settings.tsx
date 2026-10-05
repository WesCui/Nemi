"use client";

import { useEffect, useRef, useState } from "react";
import { api } from "@/lib/api";

const services: Record<string, { name: string; help: string; label: string; consent: string }> = {
  search: { name: "联网搜索", help: "需要博查开放平台的 API Key。模型 Key 不能用于搜索服务。", label: "我的联网搜索", consent: "允许妮米按我的任务查询博查，并把搜索结果提供给我选择的模型。调用消耗博查账号的额度。" },
  amap: { name: "高德地图", help: "需要高德开放平台的 Web 服务 Key，前端地图 Key 不能替代。", label: "我的高德地图", consent: "允许妮米按我的任务向高德查询地点和路线，并把结果提供给我选择的模型。调用受高德账号额度限制。" },
  mail: { name: "邮箱", help: "支持 QQ、Foxmail、163 和 126。先在邮箱设置开启 IMAP，获取客户端授权码。请填写授权码，不要填写邮箱登录密码。", label: "我的邮箱", consent: "允许妮米按我的任务读取收件箱标题和选定邮件正文，并把这些资料发送给我选择的模型。不会发送邮件、删除或标为已读，也不会在后台扫描。" },
};
type Connection = { configured: boolean; revision: number; label: string; verified_at?: string | null };

export function ServiceSettings({ id, forceEdit, afterRevision = 0, onConnected }: { id: string; forceEdit?: boolean; afterRevision?: number; onConnected: () => Promise<void> }) {
  const service = services[id];
  const [connection, setConnection] = useState<Connection | null>(null);
  const [label, setLabel] = useState(service?.label || "");
  const [key, setKey] = useState(""), [email, setEmail] = useState(""), [code, setCode] = useState("");
  const [consent, setConsent] = useState(false), [busy, setBusy] = useState(false), [error, setError] = useState("");
  const command = useRef("");
  useEffect(() => {
    let active = true;
    api<Connection>(`/connections/services/${id}`).then(c => { if (active) { setConnection(c); setLabel(c.label || service.label); } }).catch((e: Error) => { if (active) setError(e.message); });
    return () => { active = false; };
  }, [id, service]);
  if (!service) return <p>此连接暂不支持配置。</p>;
  async function save(e: React.FormEvent) {
    e.preventDefault(); if (busy || !connection) return;
    setBusy(true); setError("");
    try {
      command.current ||= crypto.randomUUID();
      const result = await api<{ revision: number }>(`/connections/services/${id}`, { label, expected_revision: connection.revision, enabled: true, confirmed: consent, ...(id === "mail" ? { email: email.trim(), authorization_code: code } : { api_key: key }) }, "PUT", command.current);
      setConnection({ configured: true, label, revision: result.revision }); setKey(""); setCode(""); setEmail(""); command.current = "";
      await onConnected();
    } catch (e) { setError((e as Error).message); }
    finally { setBusy(false); }
  }
  const ready = connection?.configured && (!forceEdit || connection.revision > afterRevision);
  return <div className="service-settings">
    <p>{service.help}</p>
    {error && <p className="form-error" role="alert">{error}</p>}
    {!connection && !error && <p role="status">正在读取连接…</p>}
    {ready ? <><p role="status">「{connection.label}」配置已保存，{connection.verified_at ? "已有成功读取记录。" : "实际调用后才会验证凭据与权限。"}</p><button type="button" className="primary" disabled={busy} onClick={() => void onConnected().catch((e: Error) => setError(e.message))}>完成连接，继续对话</button></> : connection && <form onSubmit={e => void save(e)}>
      <label>连接名称<input value={label} onChange={e => { setLabel(e.target.value); command.current = ""; }} maxLength={60} required autoComplete="off" /></label>
      {id === "mail" ? <><label>邮箱地址<input type="email" value={email} onChange={e => { setEmail(e.target.value); command.current = ""; }} required autoComplete="off" placeholder="你的邮箱地址" /></label><label>客户端授权码<input type="password" value={code} onChange={e => { setCode(e.target.value); command.current = ""; }} required minLength={8} maxLength={128} autoComplete="new-password" /></label></> : <label>{id === "amap" ? "Web 服务 Key" : "博查 API Key"}<input type="password" value={key} onChange={e => { setKey(e.target.value); command.current = ""; }} required minLength={16} maxLength={512} autoComplete="new-password" /></label>}
      <label className="check-row"><input type="checkbox" checked={consent} onChange={e => { setConsent(e.target.checked); command.current = ""; }} required /><span>{service.consent}</span></label>
      <button type="submit" className="primary" disabled={busy || !consent}>{busy ? "正在保存…" : "授权并保存连接"}</button>
    </form>}
    <small>凭据加密保存，不提供给模型，也不会显示在聊天记录中。填写后无需重新描述刚才的任务。</small>
  </div>;
}
