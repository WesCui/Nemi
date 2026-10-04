"use client";

import { useEffect, useRef, useState } from "react";
import { Download, FileText, LoaderCircle, X } from "lucide-react";
import { api, APIError } from "@/lib/api";

export type ChatFile = { id: string; name: string; kind: string; mime: string; size: number; origin_url?: string; created_at: string };
type Content = { text: string; tables: { name: string; rows: string[][] }[] };
export async function upload(file: File): Promise<ChatFile> {
  if (file.size > 5 * 1024 * 1024) throw new Error("单个文件最多 5 MB，请提供需要的部分。");
  const form = new FormData(); form.append("file", file);
  const response = await fetch("/api/v1/files", { method: "POST", credentials: "same-origin", headers: { "Idempotency-Key": crypto.randomUUID() }, body: form });
  const data = await response.json().catch(() => ({ error: "暂时无法上传文件" }));
  if (!response.ok) throw new APIError(data.error || "上传失败", response.status);
  return data as ChatFile;
}
export function FileCard({ file, onRemove }: { file: ChatFile; onRemove?: () => void }) {
  const [open, setOpen] = useState(false);
  const [content, setContent] = useState<Content | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const dialog = useRef<HTMLDialogElement>(null);
  useEffect(() => { if (open) dialog.current?.showModal(); else dialog.current?.close(); }, [open]);
  async function preview() {
    setOpen(true); setError("");
    if (content) return;
    setBusy(true);
    try { const r = await api<{ content: Content }>(`/files/${file.id}/preview`); setContent(r.content); }
    catch (e) { setError((e as Error).message); } finally { setBusy(false); }
  }
  return <>
    <div className="chat-file-card">
      <button type="button" className="chat-file-preview" onClick={() => void preview()} aria-label={`预览 ${file.name}`}><FileText size={17} /><span>{file.name}<small>{file.kind === "web" ? "网页来源" : file.kind === "artifact" ? "妮米生成" : "已上传"} · {Math.max(1, Math.ceil(file.size / 1024))} KB</small></span></button>
      {onRemove ? <button type="button" className="icon" aria-label={`移除附件 ${file.name}`} onClick={onRemove}><X size={14} /></button> : <a className="icon" href={`/api/v1/files/${file.id}/download`} aria-label={`下载 ${file.name}`}><Download size={15} /></a>}
    </div>
    <dialog ref={dialog} className="file-dialog" aria-label={`${file.name}资料预览`} onClose={() => setOpen(false)} onClick={(e) => { if (e.target === dialog.current) setOpen(false); }}>
      <header><div><span className="section-kicker">资料预览</span><h3>{file.name}</h3></div><button type="button" className="icon" aria-label="关闭资料预览" onClick={() => setOpen(false)}><X size={18} /></button></header>
      {busy && <p><LoaderCircle size={15} className="spin" />正在读取…</p>}
      {error && <p className="form-error" role="alert">{error}</p>}
      {file.origin_url && <p className="file-source">来源：<a href={file.origin_url} target="_blank" rel="noopener noreferrer">{file.origin_url}</a></p>}
      {content?.text && <><pre className="file-text">{Array.from(content.text).slice(0, 12000).join("")}</pre>{Array.from(content.text).length > 12000 && <p>预览显示前 12000 个字符，下载可查看完整正文。</p>}</>}
      {content?.tables.map((table, i) => <section key={i}><h4>{table.name} · {table.rows.length} 行</h4><div className="file-table"><table><tbody>{table.rows.slice(0, 100).map((row, r) => <tr key={r}>{row.map((cell, c) => <td key={c}>{cell}</td>)}</tr>)}</tbody></table></div>{table.rows.length > 100 && <p>预览显示前 100 行，统计工具会读取完整表格。</p>}</section>)}
      <footer><a className="primary" href={`/api/v1/files/${file.id}/download`}><Download size={14} />下载文件</a><small>正文和表格以静态内容预览。</small></footer>
    </dialog>
  </>;
}
