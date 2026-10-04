"use client";

import { useEffect, useRef, useState } from "react";
import {
  ArrowDownToLine,
  ArrowUpRight,
  CalendarDays,
  Check,
  CircleHelp,
  ChevronRight,
  MapPin,
  Search,
  Send,
  X,
} from "lucide-react";
import { api, Matter } from "@/lib/api";
import { BotSettings } from "@/components/bot-settings";
import { FeishuDocuments } from "@/components/feishu-documents";

type Channel = {
  id: string;
  name: string;
  label: string;
  state: "configured" | "invalid" | "unconfigured";
  revision?: number;
  verified_at?: string | null;
};
type App = {
  id: string;
  name: string;
  mark: string;
  color: string;
  category: "life" | "work";
  kind: "map" | "calendar" | "bot" | "planned";
  summary: string;
  scenario: string;
  boundary: string;
  url: string;
};
const apps: App[] = [
  {
    id: "amap",
    name: "高德地图",
    mark: "高",
    color: "#49775c",
    category: "life",
    kind: "map",
    summary: "查地点，准备出行",
    scenario:
      "查找目的地、餐厅或办事地点。在高德中核对营业时间、路线与预约信息。",
    boundary:
      "点击后将关键词和城市交给高德。Nemi 不获取定位，也不会读取你的行程记录。",
    url: "https://lbs.amap.com/api/uri-api/guide/search/search",
  },
  {
    id: "calendar",
    name: "系统日历",
    mark: "31",
    color: "#a46445",
    category: "life",
    kind: "calendar",
    summary: "把重要时间带到日历",
    scenario:
      "把事项的下一次提醒或截止时间导出为 .ics 文件，导入支持该格式的日历。",
    boundary:
      "每次导出一个 15 分钟事件。后续修改不会自动同步，通知方式由你的日历决定。",
    url: "https://www.rfc-editor.org/rfc/rfc5545",
  },
  {
    id: "feishu",
    name: "飞书",
    mark: "飞",
    color: "#4567a0",
    category: "work",
    kind: "bot",
    summary: "读取文档，发送事项清单",
    scenario: "导入已授权的文档资料，或把准备清单与跟进事项发到选定群。",
    boundary:
      "文档读取需独立配置并授权自建应用；群机器人只发送文字。聊天、日历与双向对话仍待接入。",
    url: "https://open.feishu.cn/document/client-docs/bot-v3/add-custom-bot",
  },
  {
    id: "wecom",
    name: "企业微信",
    mark: "企",
    color: "#4e7495",
    category: "work",
    kind: "bot",
    summary: "发送材料与跟进安排",
    scenario:
      "发送一份经你核对的办事清单或工作交接内容。群里的其他成员也能看到消息。",
    boundary: "当前仅支持配置好的群机器人，不读取个人微信聊天或企业通讯录。",
    url: "https://cloud.tencent.com/document/product/1813/130774",
  },
  {
    id: "dingtalk",
    name: "钉钉",
    mark: "钉",
    color: "#426d92",
    category: "work",
    kind: "bot",
    summary: "把行动清单发送到群",
    scenario: "将工作跟进、会议准备或材料清单发送到指定钉钉群。",
    boundary:
      "当前支持加签群机器人发送文字；待办、日历、文档和接收消息需后续接入。",
    url: "https://open.dingtalk.com/document/group/custom-robot-access",
  },
  {
    id: "wechat",
    name: "微信",
    mark: "微",
    color: "#538064",
    category: "life",
    kind: "planned",
    summary: "生活事项与订阅提醒",
    scenario: "优先考虑小程序中的事项提交，以及用户主动订阅的提醒。",
    boundary:
      "尚未接入。小程序订阅消息需要平台配置和用户同意，不能据此读取个人聊天记录。",
    url: "https://developers.weixin.qq.com/miniprogram/dev/framework/open-ability/subscribe-message.html",
  },
  {
    id: "mail",
    name: "QQ / 163 邮箱",
    mark: "邮",
    color: "#917450",
    category: "life",
    kind: "planned",
    summary: "从邮件整理账单与行程",
    scenario:
      "从用户选定的行程确认、缴费通知和材料邮件提取待办。现阶段可手动粘贴正文。",
    boundary:
      "尚未接入。自动读取需要单独配置邮箱授权和范围，不使用邮箱登录密码。",
    url: "https://service.mail.qq.com/",
  },
  {
    id: "tencent-docs",
    name: "腾讯文档",
    mark: "文",
    color: "#52739d",
    category: "work",
    kind: "planned",
    summary: "从选定文档整理待办",
    scenario: "整理一份指定文档里的材料要求、会议行动项或共同出行安排。",
    boundary:
      "尚未接入。需要应用资格与文档授权；粘贴网址不会让 Nemi 获得文档访问权限。",
    url: "https://docs.qq.com/open/",
  },
  {
    id: "wps",
    name: "WPS",
    mark: "W",
    color: "#a25c59",
    category: "work",
    kind: "planned",
    summary: "处理选定办公资料",
    scenario: "围绕选定的材料清单与表格整理准备步骤。现阶段可粘贴文字。",
    boundary:
      "尚未接入。在线文档访问与编辑需评估官方应用接口及授权，首版不自动修改原文件。",
    url: "https://open.wps.cn/documents/app-integration-dev/wps365/server/certification-authorization/user-authorization/flow",
  },
  {
    id: "rail",
    name: "铁路 12306",
    mark: "铁",
    color: "#586d7f",
    category: "life",
    kind: "planned",
    summary: "出发前准备与时间提醒",
    scenario: "根据你提供的车次和时间准备行李、证件与出发提醒。",
    boundary: "尚未接入订单。车次、票价、购票与退改需要在官方平台核对操作。",
    url: "https://www.12306.cn/",
  },
  {
    id: "meituan",
    name: "美团 / 大众点评",
    mark: "团",
    color: "#937e38",
    category: "life",
    kind: "planned",
    summary: "聚会与周末安排",
    scenario: "把选好的餐厅、地址和预约时间整理成一份出行安排。",
    boundary:
      "尚未接入个人订单和评价。店铺信息需自行核验，Nemi 不代下单或付款。",
    url: "https://open.meituan.com/",
  },
  {
    id: "shopping",
    name: "淘宝 / 京东",
    mark: "购",
    color: "#a36c48",
    category: "life",
    kind: "planned",
    summary: "购物清单与售后提醒",
    scenario: "根据你提供的购买记录整理收货、退换货期限与待办。",
    boundary: "尚未接入个人订单。商家开放接口不能直接当作个人购物账号的授权。",
    url: "https://open.jd.com/",
  },
];
const stateLabels = {
  configured: "已配置 · 未验证",
  invalid: "配置待检查",
  unconfigured: "待配置",
};

export function amapSearch(keyword: string, city: string) {
  const q = new URLSearchParams({
    keyword: keyword.trim(),
    view: "list",
    src: "Nemi",
    callnative: "1",
  });
  if (city.trim()) q.set("city", city.trim());
  return `https://uri.amap.com/search?${q}`;
}

function MapSearch({ initial = "" }: { initial?: string }) {
  const [keyword, setKeyword] = useState(initial),
    [city, setCity] = useState("");
  return (
    <div className="app-tool">
      <label>
        地点关键词
        <input
          value={keyword}
          onChange={(e) => setKeyword(e.target.value)}
          maxLength={100}
          placeholder="例如：西湖博物馆"
        />
      </label>
      <label>
        城市（可选）
        <input
          value={city}
          onChange={(e) => setCity(e.target.value)}
          maxLength={40}
          placeholder="例如：杭州"
        />
      </label>
      <a
        className={`primary external-action ${!keyword.trim() ? "disabled" : ""}`}
        aria-disabled={!keyword.trim()}
        href={keyword.trim() ? amapSearch(keyword, city) : undefined}
        target="_blank"
        rel="noopener noreferrer"
      >
        <MapPin size={15} />
        打开高德
        <ArrowUpRight size={15} />
      </a>
    </div>
  );
}

function CalendarExport({ matters }: { matters: Matter[] }) {
  const [id, setId] = useState(""),
    [error, setError] = useState(""),
    [busy, setBusy] = useState(false);
  async function download() {
    setBusy(true);
    setError("");
    try {
      const r = await fetch(`/api/v1/matters/${id}/calendar`, {
        credentials: "same-origin",
        cache: "no-store",
      });
      if (!r.ok) throw new Error((await r.json()).error || "暂时无法导出");
      const url = URL.createObjectURL(await r.blob());
      const a = document.createElement("a");
      a.href = url;
      a.download = "nemi-event.ics";
      a.click();
      URL.revokeObjectURL(url);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="app-tool">
      <label>
        选择事项
        <select
          value={id}
          onChange={(e) => {
            setId(e.target.value);
            setError("");
          }}
        >
          <option value="">选择一件正在跟进的事</option>
          {matters
            .filter((m) => m.status === "ACTIVE")
            .map((m) => (
              <option key={m.id} value={m.id}>
                {m.title}
              </option>
            ))}
        </select>
      </label>
      <button
        className="primary"
        disabled={!id || busy}
        onClick={() => void download()}
      >
        <ArrowDownToLine size={15} />
        {busy ? "正在导出" : "下载日历文件"}
      </button>
      {error && (
        <p role="alert" className="form-error">
          {error}
        </p>
      )}
    </div>
  );
}

function MessageForm({
  channel,
  matters,
}: {
  channel: Channel;
  matters: Matter[];
}) {
  const [text, setText] = useState(""),
    [confirmed, setConfirmed] = useState(false),
    [busy, setBusy] = useState(false),
    [status, setStatus] = useState(""),
    [error, setError] = useState("");
  const key = useRef("");
  const bytes = new TextEncoder().encode(text).length;
  function change(value: string) {
    setText(value);
    setConfirmed(false);
    key.current = "";
  }
  async function send() {
    if (!key.current) key.current = crypto.randomUUID();
    setBusy(true);
    setError("");
    try {
      const result = await api<{ status: string }>(
        `/connections/${channel.id}/messages`,
        { text, confirmed, config_revision: channel.revision || 0 },
        "POST",
        key.current,
      );
      setStatus(result.status);
    } catch (e) {
      if ((e as { status?: number }).status) setError((e as Error).message);
      else setStatus("UNKNOWN");
    } finally {
      setBusy(false);
    }
  }
  if (channel.state !== "configured")
    return (
      <div className="app-setup">
        <h3>先设置接收群</h3>
        <p>
          在目标群创建自定义机器人，填写下方配置即可使用。钉钉需启用加签安全设置。
        </p>
        <p>配置完成后，这里会显示接收群。当前未向任何平台发送消息。</p>
      </div>
    );
  return (
    <div className="app-tool message-tool">
      <div className="recipient">
        <Send size={16} />
        <div>
          <small>接收群 · {channel.name}</small>
          <b>{channel.label}</b>
        </div>
      </div>
      {!status && (
        <>
          <button
            className="text-button"
            disabled={busy}
            onClick={() =>
              change(
                "Nemi 应用连接检查：如果你在目标群看到这条消息，说明发送通道已经连通。",
              )
            }
          >
            填写联调消息
          </button>
          <label>
            从事项填写（可选）
            <select
              defaultValue=""
              disabled={busy}
              onChange={(e) => {
                const m = matters.find((x) => x.id === e.target.value);
                if (m)
                  change(
                    `${m.title}\n\n${m.items.map((x) => `${x.done ? "✓" : "·"} ${x.text}`).join("\n")}\n\n来自 Nemi · 请核对清单内容。`,
                  );
              }}
            >
              <option value="">选择一份事项清单</option>
              {matters.map((m) => (
                <option key={m.id} value={m.id}>
                  {m.title}
                </option>
              ))}
            </select>
          </label>
          <label>
            发送内容
            <textarea
              value={text}
              disabled={busy}
              onChange={(e) => change(e.target.value)}
              rows={7}
              maxLength={1800}
              placeholder="写下要发送的内容，发送前请确认接收群。"
            />
          </label>
          <small className={bytes > 1800 ? "form-error" : "muted"}>
            {bytes} / 1800 字节
          </small>
          <div className="message-preview">
            <small>发送预览</small>
            <p>{text || "填写后将在这里预览。"}</p>
          </div>
          <label className="check-label">
            <input
              type="checkbox"
              checked={confirmed}
              disabled={busy}
              onChange={(e) => setConfirmed(e.target.checked)}
            />
            我确认将以上内容发送至「{channel.label}」，群内成员可见
          </label>
          <button
            className="primary"
            disabled={busy || !confirmed || !text.trim() || bytes > 1800}
            onClick={() => void send()}
          >
            <Send size={15} />
            {busy ? "正在发送" : "确认发送"}
          </button>
        </>
      )}
      {status && (
        <div
          role="status"
          className={`send-receipt ${status === "DELIVERED" ? "" : "uncertain"}`}
        >
          {status === "DELIVERED" ? (
            <Check size={17} />
          ) : (
            <CircleHelp size={17} />
          )}
          <p>
            {status === "DELIVERED"
              ? "平台已接受消息，请在接收群核对。"
              : status === "REJECTED"
                ? "平台拒绝了消息，请检查机器人安全设置。"
                : "发送结果未能确认。请先在群里核对，系统不会自动重发。"}
          </p>
          <button
            className="text-button"
            onClick={() => {
              setStatus("");
              setConfirmed(false);
              key.current = "";
            }}
          >
            新建消息
          </button>
        </div>
      )}
      {error && (
        <p role="alert" className="form-error">
          {error}
        </p>
      )}
    </div>
  );
}

export function ConnectionsPanel({
  matters,
  onImported,
}: {
  matters: Matter[];
  onImported?: (id: string) => Promise<void>;
}) {
  const [channels, setChannels] = useState<Channel[] | null>(null),
    [error, setError] = useState(""),
    [query, setQuery] = useState(""),
    [filter, setFilter] = useState("all"),
    [selected, setSelected] = useState("amap");
  async function load() {
    try {
      setChannels(
        (await api<{ channels: Channel[] }>("/connections")).channels,
      );
      setError("");
    } catch (e) {
      setError((e as Error).message);
    }
  }
  useEffect(() => {
    void load();
  }, []);
  const app = apps.find((a) => a.id === selected)!;
  function label(a: App) {
    if (a.kind === "planned") return "规划中";
    if (a.kind !== "bot") return "可使用";
    const c = channels?.find((x) => x.id === a.id);
    return c ? (c.verified_at ? "发送已验证" : stateLabels[c.state]) : "加载中";
  }
  const visible = apps.filter(
    (a) =>
      (filter === "all" || a.category === filter) &&
      a.name.toLowerCase().includes(query.toLowerCase().trim()),
  );
  const detail = useRef<HTMLElement>(null);
  function selectApp(id: string) {
    setSelected(id);
    if (window.matchMedia("(max-width: 620px)").matches)
      requestAnimationFrame(() =>
        detail.current?.scrollIntoView({ block: "start" }),
      );
  }
  return (
    <section className="connections-panel">
      <div className="page-heading">
        <div>
          <span className="section-kicker">应用目录</span>
          <h1>连接应用</h1>
          <p>地图、日历与群消息，按需使用。</p>
        </div>
        <span className="catalog-count">12 款应用 · 按能力开放</span>
      </div>
      {error && (
        <div role="alert" className="error-banner">
          {error}
          <button className="text-button" onClick={() => void load()}>
            重新加载
          </button>
        </div>
      )}
      <div className="application-toolbar">
        <div className="tabs">
          {[
            ["all", "全部"],
            ["life", "生活出行"],
            ["work", "办公协作"],
          ].map(([id, name]) => (
            <button
              className={filter === id ? "active" : ""}
              key={id}
              onClick={() => setFilter(id)}
            >
              {name}
            </button>
          ))}
        </div>
        <label className="app-search">
          <Search size={16} />
          <input
            aria-label="搜索应用"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="搜索应用"
          />
        </label>
      </div>
      <div className="application-layout">
        <div className="application-list">
          {visible.map((a) => (
            <button
              key={a.id}
              className={`application-row ${a.id === selected ? "active" : ""}`}
              aria-pressed={a.id === selected}
              onClick={() => selectApp(a.id)}
            >
              <span className="application-mark" style={{ color: a.color }}>
                {a.mark}
              </span>
              <span className="application-info">
                <b>{a.name}</b>
                <small>{a.summary}</small>
              </span>
              <span
                className={`capability-label ${a.kind === "planned" ? "planned" : ""}`}
              >
                {label(a)}
              </span>
              <ChevronRight size={15} />
            </button>
          ))}
          {!visible.length && (
            <p className="muted app-no-results">没有匹配的应用。</p>
          )}
        </div>
        <article className="application-detail" key={app.id} ref={detail}>
          <header>
            <span
              className="application-mark large"
              style={{ color: app.color }}
            >
              {app.mark}
            </span>
            <div>
              <h2>{app.name}</h2>
              <span className="capability-label">{label(app)}</span>
            </div>
          </header>
          <p className="app-scenario">{app.scenario}</p>
          {app.kind === "map" && <MapSearch />}
          {app.kind === "calendar" && <CalendarExport matters={matters} />}{" "}
          {app.kind === "bot" &&
            (channels ? (
              <>
                <BotSettings
                  key={`${app.id}-settings-${channels.find((c) => c.id === app.id)?.revision || 0}`}
                  channel={channels.find((c) => c.id === app.id)!}
                  onChanged={load}
                />
                <MessageForm
                  key={`${app.id}-${channels.find((c) => c.id === app.id)?.revision || 0}`}
                  channel={channels.find((c) => c.id === app.id)!}
                  matters={matters}
                />
              </>
            ) : (
              <p className="muted">正在读取配置…</p>
            ))}
          {app.id === "feishu" && <FeishuDocuments onImported={onImported} />}
          {app.kind === "planned" && (
            <div className="app-setup">
              <h3>接入计划</h3>
              <p>
                目前可把你选定的文字粘贴到事项中，整理清单与提醒。自动获取内容将在具备官方接口和授权后开放。
              </p>
            </div>
          )}
          <div className="app-boundary">
            <h3>使用范围</h3>
            <p>{app.boundary}</p>
            <a href={app.url} target="_blank" rel="noopener noreferrer">
              查看官方说明
              <ArrowUpRight size={14} />
            </a>
          </div>
        </article>
      </div>
    </section>
  );
}

export function MatterApplications({
  matter,
  calendarAvailable,
}: {
  matter: Matter;
  calendarAvailable: boolean;
}) {
  const [open, setOpen] = useState(false);
  return (
    <section className="matter-applications">
      <div className="detail-section-heading">
        <h3>应用操作</h3>
        <button className="text-button" onClick={() => setOpen((v) => !v)}>
          {open ? (
            <>
              <X size={14} />
              收起
            </>
          ) : (
            <>
              <MapPin size={14} />
              查地点
            </>
          )}
        </button>
      </div>
      {open && <MapSearch initial={matter.title} />}
      {calendarAvailable && (
        <a
          className="text-button calendar-download"
          href={`/api/v1/matters/${matter.id}/calendar`}
          download="nemi-event.ics"
        >
          <CalendarDays size={15} />
          导出下一次时间到日历
          <ArrowDownToLine size={14} />
        </a>
      )}
      <small>日历为单次导出；群消息可在「连接应用」中预览后发送。</small>
    </section>
  );
}
