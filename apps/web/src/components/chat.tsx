"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { ArrowUp, LoaderCircle, Paperclip, Plus, Settings2, Square } from "lucide-react";
import { api } from "@/lib/api";
import { ModelOverview, modelFailure } from "@/components/models";
import { AgentAppAction } from "@/components/agent-apps";
import { ChatFile, FileCard, upload } from "@/components/chat-files";

type Conversation = { id: string; title: string; updated_at: string };
type Step = { position: number; kind: string; name: string; status: string; error: string };
type Proposal = { id: string; kind: string; status: string; result_id: string; dispatch_status?: string; payload: { title: string; source: string; category: string; deadline?: string; reminder_at?: string; quiet: boolean; repeat: string; repeat_until?: string; app_id?: string; channel_id?: string; recipient_label?: string; text?: string } };
type Plan = { goal: string; steps: { title: string; status: string }[] };
type Turn = { run_id: string; text: string; reply: string; status: string; error: string; model: string; steps: Step[]; actions: Proposal[]; plan?: Plan; files?: ChatFile[] };
type Detail = { conversation: Conversation; turns: Turn[]; summary_through: number };
const toolNames: Record<string, string> = { model: "调用模型", context_summary: "整理历史背景", update_plan: "更新任务计划", request_connection: "准备应用连接", propose_message: "准备群消息", get_current_time: "读取北京时间", list_matters: "查询事项", get_matter: "读取事项资料", list_memories: "读取个人偏好", list_connections: "查询应用状态", read_feishu_document: "读取飞书文档", propose_matter: "准备事项与提醒提案" };
const stepStatuses: Record<string, string> = { CALLING: "进行中", SUCCEEDED: "完成", FAILED: "失败", UNKNOWN: "结果未确定" };
Object.assign(toolNames,{ list_files:"查看对话资料", read_file:"读取文件正文", read_table:"读取表格", analyze_table:"统计完整表格", create_artifact:"生成成果文件", read_webpage:"读取公开网页" });
function chinaTime(value: string) { return new Date(value).toLocaleString("zh-CN", { timeZone: "Asia/Shanghai", hour12: false }); }

function ActionCard({ action, onChanged }: { action: Proposal; onChanged: () => Promise<void> }) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const keys = useRef<Record<string, string>>({});
  async function decide(decision: string) {
    if (busy) return;
    setBusy(true); setError("");
    try {
      keys.current[decision] ||= crypto.randomUUID();
      await api(`/agent/actions/${action.id}/${decision}`, { confirmed: true }, "POST", keys.current[decision]);
      await onChanged();
    } catch (e) { setError((e as Error).message); }
    finally { setBusy(false); }
  }
  const p = action.payload;
  return <aside className="agent-action" aria-label="事项提案">
    <span className="section-kicker">{action.status === "PENDING" ? "等待你确认" : action.status === "APPROVED" ? "已确认并保存" : "已取消"}</span>
    <h3>{p.title}</h3>
    <dl>
      <div><dt>类型</dt><dd>{{ life: "生活", travel: "出行", work: "工作" }[p.category] || p.category}</dd></div>
      {p.source && <div><dt>内容</dt><dd className="agent-source">{p.source}</dd></div>}
      {p.deadline && <div><dt>截止</dt><dd>{chinaTime(p.deadline)} 北京时间</dd></div>}
      {p.reminder_at && <div><dt>站内提醒</dt><dd>{chinaTime(p.reminder_at)} 北京时间</dd></div>}
      {p.reminder_at && <div><dt>重复</dt><dd>{{ once: "一次", daily: "每天", weekly: "每周" }[p.repeat] || p.repeat}{p.repeat_until && `，截至 ${chinaTime(p.repeat_until)}`}</dd></div>}
      {p.reminder_at && p.quiet && <div><dt>免打扰</dt><dd>22:00–08:00 的提醒延至早上 08:00</dd></div>}
    </dl>
    {error && <p className="form-error" role="alert">{error}</p>}
    {action.status === "PENDING" && <div className="agent-action-buttons">
      <button type="button" className="primary" disabled={busy} onClick={() => void decide("approve")}>{busy ? "处理中…" : "确认创建"}</button>
      <button type="button" className="text-button" disabled={busy} onClick={() => void decide("dismiss")}>取消提案</button>
    </div>}
    {action.status === "PENDING" && <small>确认后保存为真实事项；提醒会出现在妮米站内。</small>}
  </aside>;
}

export function Chat({ models, draft, onDraft, onConfigure, sync, reset }: {
  models: ModelOverview; draft: string; onDraft: (text: string) => void; onConfigure: () => void; sync: number; reset: number;
}) {
  const [list, setList] = useState<Conversation[]>([]);
  const [id, setID] = useState("");
  const [detail, setDetail] = useState<Detail | null>(null);
  const [modelID, setModelID] = useState(models.default_id || (models.server_available ? "__server__" : ""));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [stopping, setStopping] = useState("");
  const [attachments, setAttachments] = useState<ChatFile[]>([]);
  const [uploading, setUploading] = useState(false);
  const fileInput = useRef<HTMLInputElement>(null);
  const stopKeys = useRef<Record<string, string>>({});
  const selected = useRef("");
  const initialized = useRef(false);
  const version = useRef(0);
  const selectionVersion = useRef(0);
  const command = useRef<{ body: string; key: string } | null>(null);
  const bottom = useRef<HTMLDivElement>(null);
  const lastReset = useRef(0);
  useEffect(() => {
    if (reset === lastReset.current) return;
    lastReset.current = reset;
    version.current++;
    selectionVersion.current++;
    selected.current = ""; initialized.current = true;
    setID(""); setDetail(null); setError(""); onDraft(""); command.current = null; setLoading(false); setAttachments([]);
  }, [reset, onDraft]);
  const load = useCallback(async () => {
    const current = ++version.current;
    try {
      const response = await api<{ conversations: Conversation[] }>("/conversations");
      if (current !== version.current) return;
      setList(response.conversations);
      if (!initialized.current) {
        initialized.current = true;
        selected.current = response.conversations[0]?.id || "";
        setID(selected.current);
      }
      const target = selected.current;
      if (target) {
        const next = await api<Detail>(`/conversations/${target}`);
        if (current === version.current && selected.current === target) setDetail(next);
      }
    } catch (e) { if (current === version.current) setError((e as Error).message); }
    finally { if (current === version.current) setLoading(false); }
  }, []);
  useEffect(() => { void load(); }, [load, sync]);
  useEffect(() => {
    if (modelID === "__server__" && models.server_available) return;
    if (models.models.some((m) => m.id === modelID)) return;
    setModelID(models.default_id || (models.server_available ? "__server__" : ""));
  }, [models, modelID]);
  const pending = detail?.turns.some((t) => ["QUEUED", "RUNNING"].includes(t.status)) || false;
  useEffect(() => {
    if (!pending) return;
    let stopped = false;
    let timer: ReturnType<typeof setTimeout>;
    const poll = async () => {
      await load();
      if (!stopped) timer = setTimeout(() => void poll(), 1500);
    };
    timer = setTimeout(() => void poll(), 1500);
    return () => { stopped = true; clearTimeout(timer); };
  }, [pending, load]);
  useEffect(() => { bottom.current?.scrollIntoView({ block: "nearest" }); }, [detail?.turns.length, pending]);
  const configured = modelID === "__server__" ? models.server_available : models.models.some((m) => m.id === modelID);
  function choose(value: string) {
    version.current++;
    selectionVersion.current++;
    selected.current = value; initialized.current = true;
    setID(value); setDetail(null); setError(""); onDraft(""); command.current = null; setAttachments([]);
    setLoading(Boolean(value)); void load();
  }
  async function send(e: React.FormEvent) {
    e.preventDefault();
    await submit(draft.trim() || (attachments.length ? "请读取我提供的附件，说明主要内容，并帮我梳理接下来可以做的事。" : ""), true);
  }
  async function submit(text: string, clearDraft: boolean) {
    if (!text || busy || pending || loading || uploading) return;
    if (!configured) { onConfigure(); return; }
    setBusy(true); setError("");
    const destination = selectionVersion.current;
    try {
      const body = { conversation_id: selected.current, text, model_id: modelID, file_ids: clearDraft ? attachments.map((f) => f.id) : [] };
      const serialized = JSON.stringify(body);
      if (command.current?.body !== serialized) command.current = { body: serialized, key: crypto.randomUUID() };
      const result = await api<{ conversation_id: string; run_id: string }>("/chat/messages", body, "POST", command.current.key);
      if (destination !== selectionVersion.current) { await load(); return; }
      selected.current = result.conversation_id; initialized.current = true; setID(result.conversation_id);
      if (clearDraft) { onDraft(""); setAttachments([]); } command.current = null;
      await load();
    } catch (e) { setError((e as Error).message); }
    finally { setBusy(false); }
  }
  async function stop(run: string) {
    if (stopping) return;
    const destination = selectionVersion.current;
    setStopping(run); setError("");
    try {
      stopKeys.current[run] ||= crypto.randomUUID();
      await api(`/chat/runs/${run}/stop`, {}, "POST", stopKeys.current[run]);
      await load();
    } catch (e) { if (destination === selectionVersion.current) setError((e as Error).message); }
    finally { setStopping(""); }
  }
  return <section className="chat-panel" aria-label="与妮米对话">
    <div className="chat-toolbar">
      <div><span className="section-kicker">NEMI</span><h2>和妮米聊聊</h2></div>
      <div className="chat-controls">
        {list.length > 0 && <select aria-label="对话记录" value={id} disabled={busy} onChange={(e) => choose(e.target.value)}>
          <option value="">新对话</option>{list.map((c) => <option key={c.id} value={c.id}>{c.title}</option>)}
        </select>}
        <button className="text-button" type="button" disabled={busy} onClick={() => choose("")}><Plus size={15} />新对话</button>
      </div>
    </div>
    {detail && detail.summary_through > 0 && <p className="chat-context-note">已整理较早的对话背景，完整记录仍保留在这里。</p>}
    {detail && <div className="chat-history" aria-label="对话内容" aria-live="polite" aria-busy={pending}>
      {detail.turns.map((turn) => <div className="chat-turn" key={turn.run_id}>
        <article className="chat-message user"><span className="chat-speaker">你</span><p>{turn.text}</p>{turn.files?.filter((f) => f.kind === "upload").map((f) => <FileCard key={f.id} file={f} />)}</article>
        <article className="chat-message assistant"><div className="chat-speaker">妮米 <small>{turn.model}</small></div>
          {turn.steps?.length > 0 && <details className="agent-steps"><summary>执行过程 · {turn.steps.length} 步</summary><ol>{turn.steps.map((step) => <li key={step.position}><span>{toolNames[step.name] || step.name}</span><small data-status={step.status}>{stepStatuses[step.status] || step.status}</small></li>)}</ol></details>}
          {turn.plan && <details className="agent-plan" open={turn.status === "RUNNING"}><summary>任务计划 · {turn.plan.goal}</summary><ol>{turn.plan.steps.map((step, i) => <li key={i}><span>{step.title}</span><small data-status={step.status}>{{ pending: "待处理", in_progress: "进行中", completed: "已完成" }[step.status] || step.status}</small></li>)}</ol><small>妮米报告的工作进度；操作是否生效，以执行结果和你的确认状态为准。</small></details>}
          {turn.status === "SUCCEEDED" ? <p>{turn.reply}</p> : turn.status === "FAILED" ? <p className="form-error" role="alert">{modelFailure(turn.error)}</p> : <p className="chat-working"><LoaderCircle className="spin" size={15} />{turn.status === "QUEUED" ? "等待模型处理…" : "妮米正在处理…"}</p>}
          {turn.files?.filter((f) => f.kind !== "upload").map((f) => <FileCard key={f.id} file={f} />)}
          {["QUEUED", "RUNNING"].includes(turn.status) && <button type="button" className="text-button chat-stop" disabled={Boolean(stopping)} onClick={() => void stop(turn.run_id)}><Square size={12} />{stopping === turn.run_id ? "正在停止…" : "停止本次任务"}</button>}
          {turn.actions?.map((action) => action.kind === "create_matter" ? <ActionCard key={action.id} action={action} onChanged={load} /> : <AgentAppAction key={action.id} action={action} onChanged={load} canContinue={!busy && !pending && !loading} onContinue={() => submit("应用连接已完成，请继续刚才的任务。", false)} />)}
        </article>
      </div>)}<div ref={bottom} />
    </div>}
    {error && <p className="form-error" role="alert">{error}</p>}
    <form className="composer" onSubmit={(e) => void send(e)}>
      {attachments.length > 0 && <div className="composer-attachments">{attachments.map((f) => <FileCard key={f.id} file={f} onRemove={() => { if (!busy && !uploading) setAttachments((list) => list.filter((v) => v.id !== f.id)); }} />)}</div>}
      <label htmlFor="composer" className="sr-only">告诉妮米你想做的事</label>
      <textarea id="composer" value={draft} onChange={(e) => onDraft(e.target.value)} maxLength={2000} rows={3} disabled={busy}
        placeholder="问妮米，或聊聊你想做的事…"
        onKeyDown={(e) => { if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) { e.preventDefault(); e.currentTarget.form?.requestSubmit(); } }} />
      <div className="composer-footer">
        <input type="file" ref={fileInput} className="sr-only" aria-label="上传对话附件" accept=".pdf,.docx,.xlsx,.csv,.txt,.md,.json" multiple onChange={(e) => {
          const chosen = Array.from(e.target.files || []); e.target.value = "";
          if (!chosen.length || uploading) return;
          if (attachments.length + chosen.length > 4) { setError("每条消息最多附加 4 个文件"); return; }
          const destination = selectionVersion.current;
          setUploading(true); setError("");
          void (async () => {
            try { for (const f of chosen) { const saved = await upload(f); if (destination === selectionVersion.current) setAttachments((list) => [...list, saved]); } }
            catch (err) { if (destination === selectionVersion.current) setError((err as Error).message); }
            finally { setUploading(false); }
          })();
        }} />
        <button type="button" className="icon attachment-button" aria-label="添加附件" title="PDF、Word、Excel 或文字资料 · 每个最多 5 MB" disabled={busy || pending || uploading || attachments.length >= 4} onClick={() => fileInput.current?.click()}>{uploading ? <LoaderCircle size={17} className="spin" /> : <Paperclip size={17} />}</button>
        <select aria-label="对话使用的模型" value={modelID} disabled={busy || pending} onChange={(e) => setModelID(e.target.value)}>
          <option value="" disabled>请选择模型</option>
          {models.server_available && <option value="__server__">服务端模型</option>}
          {models.models.map((m) => <option key={m.id} value={m.id}>{m.label} · {m.model}</option>)}
        </select>
        <button type="submit" className="send-button" aria-label="发送消息" disabled={(!draft.trim() && !attachments.length) || busy || pending || loading || uploading}>
          {busy ? <LoaderCircle size={18} className="spin" /> : <ArrowUp size={20} />}
        </button>
      </div>
    </form>
    <div className="chat-caption">
      {!configured ? <button className="text-button" onClick={onConfigure}><Settings2 size={14} />配置 API Key 后开始对话</button> : <span>直接调用所选模型 · Enter 发送，Shift + Enter 换行</span>}
      <small>本段历史与调用工具读取的资料将发送给所选模型；每次模型请求计入预算。</small>
    </div>
  </section>;
}
