"use client";

import { useState } from "react";
import { ArrowUpRight, Check, LoaderCircle, Plus, Trash2 } from "lucide-react";
import { api, Run } from "@/lib/api";

type Provider = { id: string; name: string; docs: string; console: string };
export type PersonalModel = {
  id: string;
  label: string;
  provider: string;
  model: string;
  input_price_micro_cny: number;
  output_price_micro_cny: number;
  verified_at: string | null;
};
export type ModelOverview = {
  models: PersonalModel[];
  providers: Provider[];
  default_id: string;
  server_available: boolean;
};
export function ModelChoice({
  data,
  value,
  onChange,
  disabled = false,
  label = "整理使用的模型",
}: {
  data: ModelOverview;
  value: string;
  onChange: (id: string) => void;
  disabled?: boolean;
  label?: string;
}) {
  return (
    <label>
      {label}
      <select
        aria-label={label}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        disabled={disabled}
      >
        <option value="" disabled>请选择模型</option>
        {data.server_available && <option value="__server__">服务端模型</option>}
        {data.models.map((m) => (
          <option key={m.id} value={m.id}>
            {m.label} · {m.model}
          </option>
        ))}
      </select>
      <small className="muted">
        可在首页「模型配置」中添加。使用个人密钥时，资料与所选偏好会发送至对应服务商。
      </small>
    </label>
  );
}
const errors: Record<string, string> = {
  MODEL_NOT_CONFIGURED: "请先配置模型和 API Key。",
  MODEL_HTTP_401: "密钥未通过认证，请检查后重新配置。",
  MODEL_HTTP_403: "当前密钥没有权限使用此模型。",
  MODEL_HTTP_404: "服务商未找到此模型，请核对模型 ID。",
  MODEL_HTTP_429: "服务商额度或速率受限，请在控制台检查。",
  MODEL_OUTCOME_UNKNOWN:
    "调用结果无法确认，请先核对服务商账单。系统不会自动重试。",
  MODEL_INVALID_PLAN: "模型返回内容未符合清单格式，请选用支持文本对话的模型。",
  MODEL_CONFIG_REVOKED: "配置已移除，此次调用已停止。",
  MODEL_CONFIG_CHANGED: "执行配置已更新，本次任务已停止，请重新发送。",
  RUN_BUDGET_EXCEEDED: "预估费用超过单次 1 元，请核对单价或选择其他模型。",
  DAILY_BUDGET_EXCEEDED: "当天费用预算已达到 3 元。",
  MODEL_INVALID_TOOL_CALL: "模型返回了无效的工具调用，请选择支持工具调用的模型。",
  MODEL_INVALID_USAGE: "模型没有返回有效的用量，费用结果待核对，此请求不会自动重试。",
  MODEL_INVALID_RESPONSE: "模型返回的内容不完整，请核对所选模型是否支持工具调用。",
  AGENT_STEP_LIMIT_OR_INTERRUPTED: "本次处理已停止。可以缩小任务后继续，已生成的提案仍可确认。",
  AGENT_CONTEXT_TOO_LARGE: "本次任务的资料过长，请缩小范围或开始新对话。",
  AGENT_LEDGER_UNAVAILABLE: "执行记录暂时无法保存。请稍后查看结果，此请求不会自动重试。",
  AGENT_NO_LONGER_ACTIVE: "本次任务已停止，已完成的操作可在执行记录中查看。",
  AGENT_CANCELLED: "已停止本次任务。已产生的提案仍可查看；已经提交的模型请求可能产生费用。",
  AGENT_TOOL_LOOP_DETECTED: "重复调用未能推进任务，本次处理已停止。请补充信息或调整要求后继续。",
  AGENT_INVALID_SUMMARY: "模型未能生成有效的历史摘要，本次任务已停止，聊天原文仍保留。",
  AGENT_CHECKPOINT_UNAVAILABLE: "历史整理结果暂时无法保存，本次任务已停止，请稍后检查。",
};
export function modelFailure(code: string) { return errors[code] || "模型未能完成回复，请检查 Key、型号及服务商额度。结果不确定的请求不会自动重发。"; }
export function ModelSettings({
  data,
  runs,
  onChanged,
}: {
  data: ModelOverview;
  runs: Run[];
  onChanged: () => Promise<void>;
}) {
  const [adding, setAdding] = useState(data.models.length === 0);
  const [provider, setProvider] = useState("qwen");
  const [label, setLabel] = useState("");
  const [model, setModel] = useState("");
  const [key, setKey] = useState("");
  const [inputPrice, setInputPrice] = useState("");
  const [outputPrice, setOutputPrice] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [check, setCheck] = useState("");
  const [confirmed, setConfirmed] = useState(false);
  const [checking, setChecking] = useState<Record<string, string>>({});
  const currentProvider = data.providers.find((p) => p.id === provider);
  async function action(fn: () => Promise<unknown>) {
    setBusy(true);
    setError("");
    try {
      await fn();
      await onChanged();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  function price(s: string) {
    if (!/^\d{1,4}(\.\d{1,6})?$/.test(s))
      throw new Error("请填写每百万 Token 的人民币单价，最多保留六位小数。");
    const [whole, fraction = ""] = s.split(".");
    return Number(whole) * 1000000 + Number(fraction.padEnd(6, "0"));
  }
  async function save(e: React.FormEvent) {
    e.preventDefault();
    await action(async () => {
      await api("/models", {
        label,
        provider,
        model,
        api_key: key,
        input_price_micro_cny: price(inputPrice),
        output_price_micro_cny: price(outputPrice),
      });
      setKey("");
      setLabel("");
      setModel("");
      setAdding(false);
    });
  }
  return (
    <section className="model-settings">
      <p className="modal-intro">
        填入自己的 API Key 后即可对话。首个模型会设为默认，也可以在每次发送或整理时单独选择。
      </p>
      <div className="model-default">
        <label>
          首页默认模型
          <select
            aria-label="首页默认模型"
            value={data.default_id}
            disabled={busy}
            onChange={(e) =>
              void action(() =>
                api("/models/default", { model_id: e.target.value }, "PUT"),
              )
            }
          >
            <option value="">
              {data.server_available ? "服务端模型" : "请选择默认模型"}
            </option>
            {data.models.map((m) => (
              <option value={m.id} key={m.id}>
                {m.label} · {m.model}
              </option>
            ))}
          </select>
        </label>
        <small>
          切换只影响新任务。移除配置会停止尚未开始的调用；已发起的调用可能继续完成。
        </small>
      </div>
      <div className="model-list">
        {data.models.map((m) => {
          const run = runs.find((r) => r.id === checking[m.id]);
          return (
            <article className="model-row" key={m.id}>
              <div className="model-row-heading">
                <div>
                  <b>{m.label}</b>
                  <small>
                    {data.providers.find((p) => p.id === m.provider)?.name} ·{" "}
                    {m.model}
                  </small>
                </div>
                <span
                  className={`capability-label ${m.verified_at ? "" : "planned"}`}
                >
                  {m.verified_at ? "调用已验证" : "未验证"}
                </span>
              </div>
              <div className="model-row-actions">
                <small>
                  输入 ￥{m.input_price_micro_cny / 1000000} / 输出 ￥
                  {m.output_price_micro_cny / 1000000} · 每百万 Token
                </small>
                <button
                  className="text-button"
                  disabled={
                    busy ||
                    (!!run && ["QUEUED", "RUNNING"].includes(run.status))
                  }
                  onClick={() => {
                    setCheck(m.id);
                    setConfirmed(false);
                  }}
                >
                  检查连接
                </button>
                <button
                  className="icon-button"
                  aria-label={`移除 ${m.label}`}
                  disabled={busy}
                  onClick={() =>
                    void action(() => api(`/models/${m.id}`, {}, "DELETE"))
                  }
                >
                  <Trash2 size={15} />
                </button>
              </div>
              {check === m.id && (
                <div className="model-check">
                  <p>
                    发送固定的检查题，不包含你的事项资料。使用你的 API
                    额度；按所填单价控制单次 1 元、当天 3 元预算。
                  </p>
                  <label className="check-label">
                    <input
                      type="checkbox"
                      checked={confirmed}
                      onChange={(e) => setConfirmed(e.target.checked)}
                      disabled={busy}
                    />
                    我确认进行一次真实模型调用
                  </label>
                  <button
                    className="secondary"
                    disabled={!confirmed || busy}
                    onClick={() =>
                      void action(async () => {
                        const r = await api<Run>(`/models/${m.id}/check`, {
                          confirmed: true,
                        });
                        setChecking((old) => ({ ...old, [m.id]: r.id }));
                        setCheck("");
                        setConfirmed(false);
                      })
                    }
                  >
                    确认检查
                  </button>
                </div>
              )}
              {run && (
                <p className="model-receipt" role="status">
                  {run.status === "SUCCEEDED"
                    ? "检查通过，已收到有效清单。"
                    : run.status === "FAILED"
                      ? errors[run.error] ||
                        `检查未完成（${run.error}）。请核对服务商支持的模型与额度。`
                      : "正在后台检查连接…"}
                </p>
              )}
            </article>
          );
        })}
      </div>
      {!adding && (
        <button
          className="secondary"
          disabled={busy || data.models.length >= 12}
          onClick={() => setAdding(true)}
        >
          <Plus size={16} />
          添加模型
        </button>
      )}
      {adding && (
        <form className="matter-form model-add" onSubmit={(e) => void save(e)}>
          <div className="detail-section-heading">
            <h3>添加个人模型</h3>
            {data.models.length > 0 && (
              <button
                type="button"
                className="text-button"
                onClick={() => {
                  setAdding(false);
                  setKey("");
                }}
              >
                收起
              </button>
            )}
          </div>
          <div className="form-grid">
            <label>
              服务商
              <select
                aria-label="服务商"
                value={provider}
                disabled={busy}
                onChange={(e) => {
                  setProvider(e.target.value);
                  setModel("");
                  setKey("");
                }}
              >
                {data.providers.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.name}
                  </option>
                ))}
              </select>
            </label>
            <label>
              配置名称
              <input
                value={label}
                disabled={busy}
                onChange={(e) => setLabel(e.target.value)}
                placeholder="例如：日常助手"
                maxLength={60}
                required
              />
            </label>
          </div>
          <label>
            模型 ID
            <input
              value={model}
              disabled={busy}
              onChange={(e) => setModel(e.target.value.trim())}
              placeholder="填写控制台中已开通的文本模型 ID"
              maxLength={128}
              required
            />
          </label>
          <label>
            API Key
            <input
              type="password"
              autoComplete="new-password"
              value={key}
              disabled={busy}
              onChange={(e) => setKey(e.target.value)}
              placeholder="仅用于此服务商，保存后不再回显"
              maxLength={4096}
              required
            />
          </label>
          <p className="muted model-key-note">
            密钥加密保存在服务端。保存不会调用模型；更新密钥可新建配置后移除旧配置。
          </p>
          <div className="form-grid">
            <label>
              输入单价 · 元 / 百万 Token
              <input
                type="number"
                min="0.000001"
                max="1000"
                step="0.000001"
                value={inputPrice}
                onChange={(e) => setInputPrice(e.target.value)}
                placeholder="按服务商公布价格填写"
                required
                disabled={busy}
              />
            </label>
            <label>
              输出单价 · 元 / 百万 Token
              <input
                type="number"
                min="0.000001"
                max="1000"
                step="0.000001"
                value={outputPrice}
                onChange={(e) => setOutputPrice(e.target.value)}
                placeholder="按服务商公布价格填写"
                required
                disabled={busy}
              />
            </label>
          </div>
          <small className="muted">
            用于预估调用费用。服务商实际账单为准；暂不扣减缓存优惠。
          </small>
          <div className="model-form-actions">
            <a
              href={currentProvider?.console}
              target="_blank"
              rel="noopener noreferrer"
            >
              服务商控制台 <ArrowUpRight size={13} />
            </a>
            <a
              href={currentProvider?.docs}
              target="_blank"
              rel="noopener noreferrer"
            >
              接口说明 <ArrowUpRight size={13} />
            </a>
            <button className="primary" type="submit" disabled={busy}>
              {busy ? (
                <LoaderCircle size={16} className="spin" />
              ) : (
                <Check size={16} />
              )}
              保存配置
            </button>
          </div>
        </form>
      )}
      {error && (
        <p className="form-error" role="alert">
          {error}
        </p>
      )}
    </section>
  );
}
