"use client";

import { useState } from "react";
import { Check, LoaderCircle, Settings2 } from "lucide-react";
import { api } from "@/lib/api";

export function BotSettings({
  channel,
  onChanged,
  forceEdit = false,
}: {
  channel: {
    id: string;
    name: string;
    label: string;
    state: string;
    revision?: number;
    verified_at?: string | null;
  };
  onChanged: () => Promise<void>;
  forceEdit?: boolean;
}) {
  const [open, setOpen] = useState(forceEdit || channel.state !== "configured");
  const [label, setLabel] = useState(channel.label);
  const [webhook, setWebhook] = useState("");
  const [secret, setSecret] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  async function save(enabled: boolean) {
    setBusy(true);
    setError("");
    try {
      await api(
        `/connections/${channel.id}`,
        {
          label,
          webhook: enabled ? webhook : "",
          secret: enabled ? secret : "",
          enabled,
          expected_revision: channel.revision || 0,
        },
        "PUT",
      );
      setWebhook("");
      setSecret("");
      setOpen(false);
      await onChanged();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  return (
    <section className="bot-settings">
      <div className="detail-section-heading">
        <h3>接收群配置</h3>
        <button
          className="text-button"
          disabled={busy}
          onClick={() => setOpen((v) => !v)}
        >
          <Settings2 size={14} />
          {open ? "收起配置" : "修改配置"}
        </button>
      </div>
      {channel.verified_at && (
        <p className="muted">
          上次平台接受消息：
          {new Date(channel.verified_at).toLocaleString("zh-CN", {
            timeZone: "Asia/Shanghai",
          })}
        </p>
      )}
      {open && (
        <form
          className="matter-form"
          onSubmit={(e) => {
            e.preventDefault();
            void save(true);
          }}
        >
          <label>
            接收群名称
            <input
              value={label}
              onChange={(e) => setLabel(e.target.value)}
              required
              maxLength={60}
              disabled={busy}
              placeholder="填写实际接收群，发送前用于核对"
            />
          </label>
          <label>
            机器人 Webhook
            <input
              type="password"
              autoComplete="new-password"
              value={webhook}
              onChange={(e) => setWebhook(e.target.value.trim())}
              required
              maxLength={4096}
              disabled={busy}
              placeholder="从目标群的机器人设置复制完整地址"
            />
          </label>
          {channel.id !== "wecom" && (
            <label>
              加签密钥{channel.id === "feishu" && "（启用签名校验时填写）"}
              <input
                type="password"
                autoComplete="new-password"
                value={secret}
                onChange={(e) => setSecret(e.target.value.trim())}
                required={channel.id === "dingtalk"}
                disabled={busy}
                maxLength={4096}
              />
            </label>
          )}
          <small className="muted">
            请让管理员从目标群的机器人设置复制这些信息。凭据加密保存，不进入对话。保存不会发送消息；读取文档或日历需要另行授权。
          </small>
          <div className="model-form-actions">
            <button className="primary" disabled={busy} type="submit">
              {busy ? (
                <LoaderCircle size={15} className="spin" />
              ) : (
                <Check size={15} />
              )}
              保存连接
            </button>
          </div>
        </form>
      )}
      {error && (
        <p role="alert" className="form-error">
          {error}
        </p>
      )}
    </section>
  );
}
