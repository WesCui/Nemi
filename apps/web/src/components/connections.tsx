"use client";

import { useEffect, useRef, useState } from "react";
import { ArrowRight, ChevronRight, Search } from "lucide-react";
import { api, Matter } from "@/lib/api";

type Application = {
  id: string; name: string; mark: string; category: string; kind: string;
  summary: string; boundary: string; capabilities: string[]; examples: string[];
  state: string; label?: string; verified: boolean;
};
const stateLabels: Record<string, string> = { configured: "已配置", unconfigured: "待连接", available: "可使用", unavailable: "暂未启用", planned: "尚未接入" };

export function ConnectionsPanel({ onAsk }: { onAsk: (text: string) => void }) {
  const [apps, setApps] = useState<Application[]>([]);
  const [error, setError] = useState("");
  const [query, setQuery] = useState("");
  const [filter, setFilter] = useState("all");
  const [selected, setSelected] = useState("feishu_documents");
  const [loading, setLoading] = useState(true);
  const detail = useRef<HTMLElement>(null);
  async function load() {
    setLoading(true); setError("");
    try { setApps((await api<{ applications: Application[] }>("/applications")).applications); }
    catch (e) { setError((e as Error).message); }
    finally { setLoading(false); }
  }
  useEffect(() => { void load(); }, []);
  const visible = apps.filter((a) => (filter === "all" || a.category === filter) && `${a.name} ${a.id}`.toLowerCase().includes(query.trim().toLowerCase()));
  const app = visible.find((a) => a.id === selected) || visible[0];
  function select(id: string) {
    setSelected(id);
    if (window.matchMedia("(max-width: 620px)").matches) requestAnimationFrame(() => detail.current?.scrollIntoView({ block: "start" }));
  }
  return <section className="connections-panel">
    <div className="page-heading"><div><span className="section-kicker">应用</span><h1>连接应用</h1><p>把想做的事告诉妮米，连接和操作都在对话里完成。</p></div><span className="catalog-count">{apps.length} 款应用 · 按能力开放</span></div>
    {error && <div className="error-banner" role="alert">{error}<button className="text-button" onClick={() => void load()}>重新加载</button></div>}
    <div className="application-toolbar">
      <div className="tabs">{[["all", "全部"], ["life", "生活出行"], ["work", "办公协作"]].map(([id, label]) => <button key={id} className={filter === id ? "active" : ""} onClick={() => setFilter(id)}>{label}</button>)}</div>
      <label className="app-search"><Search size={16} /><input aria-label="搜索应用" value={query} onChange={(e) => setQuery(e.target.value)} placeholder="搜索应用" /></label>
    </div>
    {loading && !apps.length ? <p className="muted" role="status">正在读取应用能力…</p> : <div className="application-layout">
      <div className="application-list">{visible.map((a) => <button key={a.id} className={`application-row ${a.id === app?.id ? "active" : ""}`} aria-pressed={a.id === app?.id} onClick={() => select(a.id)}>
        <span className="application-mark" style={{ color: a.category === "work" ? "#4567a0" : "#49775c" }}>{a.mark}</span>
        <span className="application-info"><b>{a.name}</b><small>{a.summary}</small></span>
        <span className={`capability-label ${a.state === "planned" ? "planned" : ""}`}>{stateLabels[a.state] || a.state}</span><ChevronRight size={15} />
      </button>)}{!visible.length && <p className="muted app-no-results">没有匹配的应用。</p>}</div>
      {app && <article className="application-detail" key={app.id} ref={detail}>
        <header><span className="application-mark large">{app.mark}</span><div><h2>{app.name}</h2><span className="capability-label">{stateLabels[app.state] || app.state}</span></div></header>
        <p className="app-scenario">{app.summary}</p>
        {app.label && <p className="agent-recipient">当前连接：{app.label} · {app.verified ? "有成功的平台回执" : "尚未验证平台权限"}</p>}
        <div className="app-conversation-entry">
          <span className="section-kicker">可以这样说</span>
          <p>{app.examples[0]}</p>
          <button type="button" className="primary" onClick={() => onAsk(app.examples[0])}>{app.state === "planned" ? "在对话中处理我提供的资料" : "交给妮米"}<ArrowRight size={15} /></button>
          <small>回到当前对话后可以修改这句话，发送时调用你选择的模型。</small>
        </div>
        {app.state === "configured" && <div className="app-connection-actions">
          <button type="button" className="text-button" onClick={() => onAsk(`帮我修改${app.name}的连接，在对话里引导我填写新的配置。`)}>在对话中修改连接</button>
          <button type="button" className="text-button" onClick={() => onAsk(`帮我停用${app.name}的「${app.label}」连接，先让我核对。`)}>在对话中停用连接</button>
        </div>}
        <div className="app-boundary"><h3>当前能力</h3><p>{app.boundary}</p></div>
      </article>}
    </div>}
  </section>;
}

export function MatterApplications({ matter, calendarAvailable, onAsk }: { matter: Matter; calendarAvailable: boolean; onAsk: (text: string) => void }) {
  return <section className="matter-applications">
    <div className="detail-section-heading"><h3>交给妮米继续处理</h3></div>
    <button type="button" className="text-button" onClick={() => onAsk(`继续处理「${matter.title}」（事项 ID：${matter.id}），先读取真实资料和进度，问我还想做什么。`)}>在对话中跟进这件事<ArrowRight size={14} /></button>
    {calendarAvailable && <button type="button" className="text-button calendar-download" onClick={() => onAsk(`请把「${matter.title}」（事项 ID：${matter.id}）的下一次提醒或截止时间生成可下载的日历文件。`)}>让妮米生成日历文件<ArrowRight size={14} /></button>}
    <small>生成、修改、发送和确认结果，都可以在同一段对话中处理。</small>
  </section>;
}
