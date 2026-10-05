"use client";

import { useEffect, useState } from "react";
import { ArrowUpRight, FileText, LoaderCircle } from "lucide-react";
import { api, Matter } from "@/lib/api";
type State = {
  configured: boolean;
  label: string;
  revision: number;
  verified_at?: string | null;
};
export function FeishuDocuments({
  onImported,
  setupOnly = false,
  onConnected,
  forceEdit = false,
  afterRevision,
}: {
  onImported?: (id: string) => Promise<void>;
  setupOnly?: boolean;
  onConnected?: () => Promise<void>;
  forceEdit?: boolean;
  afterRevision?: number;
}) {
  const [state, setState] = useState<State | null>(null);
  const [editing, setEditing] = useState(forceEdit);
  const [label, setLabel] = useState("");
  const [appID, setAppID] = useState("");
  const [secret, setSecret] = useState("");
  const [url, setURL] = useState("");
  const [title, setTitle] = useState("");
  const [confirmed, setConfirmed] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [imported, setImported] = useState<Matter | null>(null);
  async function load() {
    try {
      const s = await api<State>("/connections/feishu/documents");
      setState(s);
      setLabel(s.label);
      setError("");
    } catch (e) {
      setError((e as Error).message);
    }
  }
  useEffect(() => {
    void load();
  }, []);
  async function action(fn: () => Promise<unknown>) {
    setBusy(true);
    setError("");
    try {
      await fn();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  return (
    <section className="feishu-documents">
      <div className="detail-section-heading">
        <h3>
          <FileText size={16} />
          读取飞书文档
        </h3>
        {state?.configured && (
          <button
            className="text-button"
            disabled={busy}
            onClick={() => setEditing((v) => !v)}
          >
            {editing ? "收起" : "应用配置"}
          </button>
        )}
      </div>
      <p className="muted">
        {setupOnly ? "连接后，把文档链接发给妮米，直接在对话里总结、分析或准备工作。首次授权需要你或飞书管理员提供应用信息。" : "把一份已授权的飞书 docx 文档导入为事项资料，再整理清单、安排提醒。"}
      </p>
      {state && (!state.configured || editing) && (
        <form
          className="matter-form"
          onSubmit={(e) => {
            e.preventDefault();
            void action(async () => {
              await api(
                "/connections/feishu/documents",
                {
                  app_id: appID,
                  app_secret: secret,
                  label,
                  expected_revision: state.revision,
                  enabled: true,
                },
                "PUT",
              );
              setSecret("");
              setAppID("");
              setEditing(false);
              await load();
              await onConnected?.();
            });
          }}
        >
          <label>
            飞书应用名称
            <input
              value={label}
              onChange={(e) => setLabel(e.target.value)}
              maxLength={60}
              required
              disabled={busy}
            />
          </label>
          <label>
            App ID
            <input
              value={appID}
              onChange={(e) => setAppID(e.target.value.trim())}
              placeholder="cli_ 开头的自建应用 ID"
              maxLength={128}
              required
              disabled={busy}
            />
          </label>
          <label>
            App Secret
            <input
              type="password"
              autoComplete="new-password"
              value={secret}
              onChange={(e) => setSecret(e.target.value.trim())}
              maxLength={4096}
              required
              disabled={busy}
            />
          </label>
          <small className="muted">
            在飞书开放平台创建、发布自建应用，开通文档读取权限，并将指定文档授权给应用。密钥加密保存；群机器人的
            Webhook 不能用于读取文档。
          </small>
          <div className="model-form-actions">
            {!setupOnly && <a
              href="https://open.feishu.cn/app"
              target="_blank"
              rel="noopener noreferrer"
            >
              飞书开放平台 <ArrowUpRight size={13} />
            </a>}
            {!setupOnly && state.configured && (
              <button
                className="text-button"
                type="button"
                disabled={busy}
                onClick={() =>
                  void action(async () => {
                    await api(
                      "/connections/feishu/documents",
                      { enabled: false, expected_revision: state.revision },
                      "PUT",
                    );
                    await load();
                    setSecret("");
                  })
                }
              >
                停用文档连接
              </button>
            )}
            <button className="primary" disabled={busy} type="submit">
              {busy && <LoaderCircle size={15} className="spin" />}保存应用配置
            </button>
          </div>
        </form>
      )}
      {setupOnly && state?.configured && !editing && (!forceEdit || state.revision > (afterRevision || 0)) && <div className="model-receipt"><p>文档连接已配置；是否有读取权限，以实际读取结果为准。</p><button className="primary" type="button" disabled={busy} onClick={() => void action(async () => { await onConnected?.(); })}>完成连接，继续对话</button></div>}
      {!setupOnly && state?.configured && !editing && (
        <form
          className="matter-form"
          onSubmit={(e) => {
            e.preventDefault();
            void action(async () => {
              const m = await api<Matter>(
                "/connections/feishu/documents/import",
                {
                  document_url: url,
                  title,
                  config_revision: state.revision,
                  confirmed,
                },
              );
              setImported(m);
              setConfirmed(false);
              await load();
            });
          }}
        >
          <small className="muted">
            使用应用「{state.label}」 ·{" "}
            {state.verified_at ? "文档读取曾验证" : "尚未验证读取权限"}
          </small>
          <label>
            飞书文档链接
            <input
              type="url"
              value={url}
              onChange={(e) => {
                setURL(e.target.value);
                setConfirmed(false);
                setImported(null);
              }}
              placeholder="https://你的企业.feishu.cn/docx/…"
              maxLength={2048}
              required
              disabled={busy}
            />
          </label>
          <label>
            导入后的事项名称
            <input
              value={title}
              onChange={(e) => {
                setTitle(e.target.value);
                setConfirmed(false);
              }}
              maxLength={100}
              required
              disabled={busy}
            />
          </label>
          <label className="check-label">
            <input
              type="checkbox"
              checked={confirmed}
              onChange={(e) => setConfirmed(e.target.checked)}
              disabled={busy}
            />
            我确认读取此文档，并将文本保存在我的 Nemi 空间
          </label>
          <small className="muted">
            仅导入当前文本，最多 12000
            字节。不会修改文档，也不会自动调用模型；后续整理时可自行选择模型。
          </small>
          <button
            className="primary"
            type="submit"
            disabled={busy || !confirmed || !!imported}
          >
            {busy ? (
              <LoaderCircle size={15} className="spin" />
            ) : (
              <FileText size={15} />
            )}
            读取并导入
          </button>
        </form>
      )}
      {imported && (
        <div className="model-receipt" role="status">
          已导入「{imported.title}」。
          {onImported && (
            <button
              className="text-button"
              onClick={() => void onImported(imported.id)}
            >
              打开事项
            </button>
          )}
        </div>
      )}
      {error && (
        <p className="form-error" role="alert">
          {error}
        </p>
      )}
    </section>
  );
}
