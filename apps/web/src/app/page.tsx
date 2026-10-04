"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import {
  ArrowDownToLine,
  ArrowRight,
  Activity,
  Bell,
  Brain,
  Check,
  CheckCheck,
  ChevronRight,
  CircleHelp,
  Clock3,
  Compass,
  FileText,
  Leaf,
  ListTodo,
  LoaderCircle,
  LogOut,
  Menu,
  Plus,
  Sun,
  X,
} from "lucide-react";
import {
  api,
  APIError,
  categories,
  Dashboard,
  formatTime,
  isoChina,
  localInput,
  Item,
  Matter,
  quietPreview,
  Reminder,
  repeatLabels,
  endOfChinaDate,
} from "@/lib/api";
import {
  ActivityPanel,
  MemoryPanel,
  RepeatFields,
  SourceEditor,
} from "@/components/assistant-next";

import { ConnectionsPanel, MatterApplications } from "@/components/connections";
import { ModelChoice, ModelOverview, ModelSettings } from "@/components/models";
import { Chat } from "@/components/chat";

type View =
  | "today"
  | "matters"
  | "reminders"
  | "connections"
  | "memory"
  | "activity";
function Brand({ compact = false }: { compact?: boolean }) {
  return (
    <div className={`brand ${compact ? "compact" : ""}`}>
      <span className="brand-mark" aria-hidden="true">
        n.
      </span>
      <span>
        Nemi<span className="brand-cn">妮米</span>
      </span>
    </div>
  );
}
function Busy() {
  return <LoaderCircle size={17} className="spin" aria-label="处理中" />;
}
function Notice({ children }: { children: React.ReactNode }) {
  return (
    <div className="notice" role="status">
      {children}
    </div>
  );
}
const runLabels: Record<string, string> = {
  QUEUED: "排队整理中",
  RUNNING: "正在整理",
  SUCCEEDED: "清单已生成",
  FAILED: "整理未完成",
};

export default function Home() {
  const [data, setData] = useState<Dashboard | null>(null);
  const [models, setModels] = useState<ModelOverview | null>(null);
  const [modelSettings, setModelSettings] = useState(false);
  const [auth, setAuth] = useState<"loading" | "login" | "ready" | "error">(
    "loading",
  );
  const [error, setError] = useState("");
  const [view, setView] = useState<View>("today");
  const [draft, setDraft] = useState("");
  const [chatReset, setChatReset] = useState(0);
  const [create, setCreate] = useState(false);
  const [selected, setSelected] = useState<string | null>(null);
  const [mobileNav, setMobileNav] = useState(false);
  const [filter, setFilter] = useState("ACTIVE");
  const refresh = useCallback(async () => {
    try {
      const [d, m] = await Promise.all([
        api<Dashboard>("/dashboard"),
        api<ModelOverview>("/models"),
      ]);
      setData(d);
      setModels(m);
      setAuth("ready");
    } catch (e) {
      if (e instanceof APIError && e.status === 401) {
        setAuth("login");
        setData(null);
      } else {
        setError(e instanceof Error ? e.message : "无法连接服务");
        setAuth((previous) => (previous === "ready" ? previous : "error"));
      }
    }
  }, []);
  useEffect(() => {
    void refresh();
  }, [refresh]);
  useEffect(() => {
    if (auth !== "ready") return;
    const events = new EventSource("/api/v1/events");
    let timer: ReturnType<typeof setTimeout> | undefined;
    events.onmessage = () => {
      clearTimeout(timer);
      timer = setTimeout(() => void refresh(), 250);
    };
    // Polling is a recovery fallback, not a second task scheduler.
    const interval = setInterval(() => void refresh(), 15000);
    const focus = () => void refresh();
    window.addEventListener("focus", focus);
    return () => {
      events.close();
      clearTimeout(timer);
      clearInterval(interval);
      window.removeEventListener("focus", focus);
    };
  }, [auth, refresh]);
  useEffect(() => {
    if (!create && !selected && !modelSettings) return;
    const handler = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        setCreate(false);
        setSelected(null);
        setModelSettings(false);
      }
    };
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, [create, selected, modelSettings]);

  if (auth === "loading")
    return (
      <div className="splash">
        <Brand />
        <Busy />
        <p>正在打开你的生活空间…</p>
      </div>
    );
  if (auth === "error")
    return (
      <div className="splash">
        <Brand />
        <h2>暂时连接不上妮米</h2>
        <p>{error}</p>
        <button
          className="primary"
          onClick={() => {
            setAuth("loading");
            void refresh();
          }}
        >
          重新连接
        </button>
      </div>
    );
  if (auth === "login" || !data || !models) return <Login onLogin={refresh} />;
  const active = data.matters.filter((m) => m.status === "ACTIVE");
  const pending = data.reminders.filter(
    (r) => r.enabled && r.sync_status !== "FIRED",
  );
  const currentMatter = data.matters.find((m) => m.id === selected);
  const nav = [
    { id: "today", title: "助手", icon: Sun },
    { id: "matters", title: "我的事项", icon: ListTodo },
    { id: "reminders", title: "提醒", icon: Bell },
    { id: "activity", title: "工作动态", icon: Activity },
    { id: "memory", title: "生活偏好", icon: Brain },
    { id: "connections", title: "连接应用", icon: Compass },
  ] as const;
  async function logout() {
    try {
      await api("/auth/logout", {});
      setData(null);
      setAuth("login");
    } catch (e) {
      setError((e as Error).message);
    }
  }
  function example(text: string) {
    setDraft(text);
    document.getElementById("composer")?.focus();
  }
  return (
    <div className="app-shell">
      <aside className={`sidebar ${mobileNav ? "nav-open" : ""}`}>
        <Brand />
        <button
          className="new-button"
          aria-label="开始新对话"
          onClick={() => {
            setView("today");
            setChatReset((value) => value + 1);
            setMobileNav(false);
          }}
        >
          <Plus size={18} />
          新对话
        </button>
        <button className="text-button new-matter-shortcut" onClick={() => { setCreate(true); setMobileNav(false); }}><ListTodo size={15} />记一件事</button>
        <nav aria-label="主导航">
          {nav.map((n) => (
            <button
              key={n.id}
              className={view === n.id ? "nav-item selected" : "nav-item"}
              onClick={() => {
                setView(n.id);
                setMobileNav(false);
              }}
            >
              <n.icon size={19} />
              {n.title}
              {n.id === "matters" && active.length > 0 && (
                <span className="nav-count">{active.length}</span>
              )}
            </button>
          ))}
        </nav>
        <div className="sidebar-note">
          <span>PERSONAL SPACE</span>
          <p>对话 · 任务 · 应用</p>
        </div>
        <div className="profile">
          <span className="avatar">你</span>
          <div>
            <b>我的个人空间</b>
            <small>事项、偏好与日常安排</small>
          </div>
          <button
            className="icon-button"
            aria-label="退出登录"
            title="退出登录"
            onClick={() => void logout()}
          >
            <LogOut size={16} />
          </button>
        </div>
      </aside>
      {mobileNav && (
        <button
          className="nav-backdrop"
          aria-label="关闭导航"
          onClick={() => setMobileNav(false)}
        />
      )}
      <main className="main">
        <header className="topbar">
          <div className="breadcrumb">
            <button
              className="mobile-menu icon-button"
              aria-label="打开导航"
              onClick={() => setMobileNav(true)}
            >
              <Menu size={22} />
            </button>
            <span>我的空间</span>
            <ChevronRight size={13} />
            <b>{nav.find((n) => n.id === view)?.title}</b>
          </div>
          <div className="topbar-right">
            <button
              className="mode-pill model-entry"
              onClick={() => setModelSettings(true)}
              aria-label="模型配置"
            >
              <i />
              {models.models.find((m) => m.id === models.default_id)?.label || "配置我的模型"}
            </button>
            <button
              className="icon-button notification-button"
              aria-label="查看站内提醒"
              onClick={() => setView("reminders")}
            >
              <Bell size={19} />
              {data.notifications.length > 0 && <i />}
            </button>
          </div>
        </header>
        <div className="content">
          {error && (
            <div className="error-banner" role="alert">
              {error}
              <button
                className="icon-button"
                aria-label="关闭提示"
                onClick={() => setError("")}
              >
                <X size={16} />
              </button>
            </div>
          )}
          {view === "today" && (
            <>
              <section className="hero">
                <div>
                  <div className="eyebrow">
                    <Today />
                  </div>
                  <h1>有什么想交给妮米？</h1>
                  <p>聊问题、整理资料，或交给我一个目标。</p>
                </div>
                <div className="hero-ledger">
                  <div>
                    <b>{active.length.toString().padStart(2, "0")}</b>
                    <span>正在跟进</span>
                  </div>
                  <div>
                    <b>{pending.length.toString().padStart(2, "0")}</b>
                    <span>待提醒</span>
                  </div>
                </div>
              </section>
              <Chat models={models} draft={draft} onDraft={setDraft} onConfigure={() => setModelSettings(true)} sync={data.activity[0]?.sequence || 0} reset={chatReset} />
              <div className="suggestions">
                <button
                  onClick={() =>
                    example("帮我准备周末的出行，整理行李和待确认的安排。")
                  }
                >
                  <Compass size={15} />
                  准备周末出行
                </button>
                <button
                  onClick={() =>
                    example("整理报名需要的材料，帮我列出还需要补充的内容。")
                  }
                >
                  <FileText size={15} />
                  整理报名材料
                </button>
                <button
                  onClick={() =>
                    example("跟进今天答应的事情，列一份行动清单。")
                  }
                >
                  <CheckCheck size={15} />
                  跟进今天的承诺
                </button>
              </div>
              <div className="section-heading">
                <div>
                  <h2>
                    正在跟进{" "}
                    <span>{active.length.toString().padStart(2, "0")}</span>
                  </h2>
                  <p>你的计划与下一步。</p>
                </div>
                <button
                  className="text-button"
                  onClick={() => setView("matters")}
                >
                  查看全部 <ArrowRight size={15} />
                </button>
              </div>
              {active.length ? (
                <div className="matter-grid">
                  {active.slice(0, 4).map((m) => (
                    <MatterCard
                      key={m.id}
                      matter={m}
                      data={data}
                      onClick={() => setSelected(m.id)}
                    />
                  ))}
                </div>
              ) : (
                <Empty onCreate={() => setCreate(true)} />
              )}
              <div className="bottom-grid">
                <section className="next-reminders">
                  <h3>
                    <Bell size={17} />
                    接下来的提醒
                  </h3>
                  {pending.length ? (
                    pending.slice(0, 3).map((r) => (
                      <button
                        className="reminder-row"
                        key={r.id}
                        onClick={() => setSelected(r.matter_id)}
                      >
                        <div className="time-tile">
                          <Clock3 size={18} />
                        </div>
                        <div>
                          <b>{r.title}</b>
                          <small>{formatTime(r.due_at)} · 站内提醒</small>
                        </div>
                        <ChevronRight size={16} />
                      </button>
                    ))
                  ) : (
                    <p className="muted">
                      保存事项时可以设置提醒。你确认过的时间，妮米会记住。
                    </p>
                  )}
                </section>
                <section className="application-shortcut">
                  <span className="section-kicker">APPLICATIONS</span>
                  <h3>常用应用</h3>
                  <p>查地点、导出日历，或把清单发到接收群。</p>
                  <button
                    className="text-button"
                    onClick={() => setView("connections")}
                  >
                    打开应用
                    <ArrowRight size={15} />
                  </button>
                </section>
              </div>
            </>
          )}
          {view === "matters" && (
            <>
              <PageHeading
                title="我的事项"
                description="从想到，到准备好，再到完成。"
                action={() => setCreate(true)}
              />
              <div className="tabs">
                {[
                  ["ACTIVE", "在跟进"],
                  ["COMPLETED", "已完成"],
                ].map(([value, label]) => (
                  <button
                    key={value}
                    className={filter === value ? "active" : ""}
                    onClick={() => setFilter(value)}
                  >
                    {label}
                    <span>
                      {data.matters.filter((m) => m.status === value).length}
                    </span>
                  </button>
                ))}
              </div>
              <div className="matter-grid">
                {data.matters
                  .filter((m) => m.status === filter)
                  .map((m) => (
                    <MatterCard
                      key={m.id}
                      matter={m}
                      data={data}
                      onClick={() => setSelected(m.id)}
                    />
                  ))}
              </div>
              {!data.matters.some((m) => m.status === filter) && (
                <Empty
                  onCreate={() => setCreate(true)}
                  text={
                    filter === "COMPLETED" ? "完成的事项会留在这里" : undefined
                  }
                />
              )}
            </>
          )}
          {view === "reminders" && (
            <>
              <PageHeading
                title="提醒"
                description="所有时间均为北京时间，站内记录会持续保存。"
              />
              <Notice>
                <CircleHelp size={17} />
                当前提供站内提醒，需要打开页面查看；微信及手机系统通知尚未接入。
              </Notice>
              <section className="reminder-list">
                <h3>待提醒 · {pending.length}</h3>
                {pending.map((r) => (
                  <button
                    className="reminder-row"
                    key={r.id}
                    onClick={() => setSelected(r.matter_id)}
                  >
                    <span className="time-tile">
                      <Bell size={18} />
                    </span>
                    <div>
                      <b>{r.title}</b>
                      <small>
                        {formatTime(r.due_at)}
                        {r.due_at !== r.nominal_at && " · 已按免打扰调整"}
                        {r.repeat !== "once" && ` · ${repeatLabels[r.repeat]}`}
                      </small>
                    </div>
                    <span className="status-badge">
                      {r.sync_status === "APPLIED" ? "已安排" : "正在同步"}
                    </span>
                    <ChevronRight size={15} />
                  </button>
                ))}
                {!pending.length && (
                  <p className="muted">暂时没有待提醒的事项。</p>
                )}
              </section>
              <section className="reminder-list">
                <h3>站内记录 · {data.notifications.length}</h3>
                {data.notifications.map((n) => (
                  <button
                    className="reminder-row"
                    key={n.id}
                    onClick={() => setSelected(n.matter_id)}
                  >
                    <span className="time-tile">
                      <Check size={18} />
                    </span>
                    <div>
                      <b>{n.title}</b>
                      <small>
                        {formatTime(n.created_at)} ·{" "}
                        {n.status === "OVERDUE"
                          ? "恢复后记录的过期提醒"
                          : "提醒已记入站内"}
                      </small>
                    </div>
                    <ChevronRight size={15} />
                  </button>
                ))}
                {!data.notifications.length && (
                  <p className="muted">提醒到时间后，记录会出现在这里。</p>
                )}
              </section>
              {data.reminders.some((r) => !r.enabled) && (
                <section className="reminder-list">
                  <h3>已停用或结束</h3>
                  {data.reminders
                    .filter((r) => !r.enabled)
                    .map((r) => (
                      <button
                        className="reminder-row"
                        key={r.id}
                        onClick={() => setSelected(r.matter_id)}
                      >
                        <Clock3 size={17} />
                        <div>
                          <b>{r.title}</b>
                          <small>
                            {repeatLabels[r.repeat]} ·{" "}
                            {r.sync_status === "ENDED"
                              ? "周期已结束"
                              : "已停用"}
                          </small>
                        </div>
                        <ChevronRight size={15} />
                      </button>
                    ))}
                </section>
              )}
            </>
          )}
          {view === "memory" && (
            <MemoryPanel memories={data.memories} refresh={refresh} />
          )}
          {view === "activity" && (
            <ActivityPanel data={data} onSelect={setSelected} />
          )}
          {view === "connections" && (
            <ConnectionsPanel
              matters={data.matters}
              onImported={async (id) => {
                await refresh();
                setSelected(id);
              }}
            />
          )}
          <footer className="page-footer">
            <span>Nemi · 妮米</span>
            <span>AI 生成内容，请核对关键资料</span>
          </footer>
        </div>
      </main>
      {modelSettings && (
        <Modal title="我的模型" onClose={() => setModelSettings(false)} wide>
          <ModelSettings data={models} runs={data.runs} onChanged={refresh} />
        </Modal>
      )}
      {create && (
        <CreateDialog
          draft={draft}
          memoryCount={data.memories.length}
          models={models}
          onClose={() => setCreate(false)}
          onSaved={async (id, warning) => {
            setCreate(false);
            setDraft("");
            await refresh();
            setSelected(id);
            if (warning) setError(warning);
          }}
        />
      )}
      {currentMatter && (
        <MatterDialog
          key={currentMatter.id}
          matter={currentMatter}
          data={data}
          models={models}
          refresh={refresh}
          onClose={() => setSelected(null)}
        />
      )}
    </div>
  );
}

function Today() {
  const [text, setText] = useState("今天");
  useEffect(
    () =>
      setText(
        new Intl.DateTimeFormat("zh-CN", {
          timeZone: "Asia/Shanghai",
          month: "long",
          day: "numeric",
          weekday: "long",
        }).format(new Date()),
      ),
    [],
  );
  return <span>{text}</span>;
}
function Login({ onLogin }: { onLogin: () => Promise<void> }) {
  const [code, setCode] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await api("/auth/login", { invite_code: code });
      await onLogin();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="login-page">
      <section className="login-story">
        <Brand />
        <div className="login-copy">
          <span className="section-kicker">A PLACE FOR YOUR EVERYDAY</span>
          <h1>
            为日常，
            <br />
            留一页。
          </h1>
          <p>
            事项、提醒与常用应用，
            <br />
            在一个安静的空间里。
          </p>
          <div className="login-index">
            <div>
              <span>01</span>记录计划
            </div>
            <div>
              <span>02</span>确认时间
            </div>
            <div>
              <span>03</span>应用协作
            </div>
          </div>
        </div>
        <small>NEMI / PERSONAL ASSISTANT</small>
      </section>
      <section className="login-form-panel">
        <div className="login-form">
          <Brand compact />
          <h2>进入个人空间</h2>
          <p>输入邀请口令，打开你的个人空间。</p>
          <form onSubmit={(e) => void submit(e)}>
            <label htmlFor="invite">邀请口令</label>
            <input
              id="invite"
              type="password"
              autoComplete="current-password"
              value={code}
              onChange={(e) => setCode(e.target.value)}
              placeholder="输入管理员提供的口令"
              required
              minLength={16}
              maxLength={128}
            />
            {error && (
              <p className="form-error" role="alert">
                {error}
              </p>
            )}
            <button className="primary full" disabled={busy}>
              {busy ? (
                <Busy />
              ) : (
                <>
                  进入我的空间
                  <ArrowRight size={17} />
                </>
              )}
            </button>
          </form>
          <div className="login-footnote">
            <CircleHelp size={15} />
            <p>你的事项、偏好与应用，放在一个空间里。</p>
          </div>
        </div>
      </section>
    </div>
  );
}
function PageHeading({
  title,
  description,
  action,
}: {
  title: string;
  description: string;
  action?: () => void;
}) {
  return (
    <div className="page-heading">
      <div>
        <h1>{title}</h1>
        <p>{description}</p>
      </div>
      {action && (
        <button className="primary" onClick={action}>
          <Plus size={16} />
          记一件事
        </button>
      )}
    </div>
  );
}
function Empty({ onCreate, text }: { onCreate: () => void; text?: string }) {
  return (
    <section className="empty-state">
      <span className="empty-icon">
        <Leaf size={26} strokeWidth={1.4} />
      </span>
      <div>
        <h3>{text || "第一件事，从这里开始"}</h3>
        <p>把脑海里的计划写下来，妮米帮你理出下一步。</p>
      </div>
      <button className="text-button" onClick={onCreate}>
        记一件事
        <Plus size={16} />
      </button>
    </section>
  );
}
function MatterCard({
  matter: m,
  data,
  onClick,
}: {
  matter: Matter;
  data: Dashboard;
  onClick: () => void;
}) {
  const run = data.runs.find((r) => r.matter_id === m.id);
  const reminder = data.reminders.find(
    (r) => r.matter_id === m.id && r.enabled && r.sync_status !== "FIRED",
  );
  const done = m.items.filter((i) => i.done).length;
  return (
    <button className={`matter-card ${m.category}`} onClick={onClick}>
      <div className="matter-card-top">
        <span className="category">
          <span className="category-icon">
            {m.category === "travel" ? (
              <Compass size={15} />
            ) : m.category === "work" ? (
              <FileText size={15} />
            ) : (
              <Leaf size={15} />
            )}
          </span>
          {categories[m.category]}
        </span>
        <span
          className={`status-badge ${m.status === "COMPLETED" ? "complete" : ""}`}
        >
          {m.status === "COMPLETED"
            ? "已完成"
            : run && ["QUEUED", "RUNNING"].includes(run.status)
              ? runLabels[run.status]
              : "在跟进"}
        </span>
      </div>
      <h3>{m.title}</h3>
      <p>
        {m.items.length
          ? m.items.find((i) => !i.done)?.text || "清单已经核对完毕"
          : m.source || "可以让妮米整理一份准备清单"}
      </p>
      <div className="card-meta">
        {m.deadline ? (
          <span>
            <Clock3 size={13} />
            {formatTime(m.deadline, true)}前
          </span>
        ) : (
          <span>
            <ListTodo size={13} />
            {m.items.length
              ? `${done} / ${m.items.length} 项完成`
              : "等待下一步"}
          </span>
        )}
        {reminder && <Bell size={14} />}
      </div>
      <div className="progress-track">
        <i
          style={{
            width: `${m.status === "COMPLETED" ? 100 : m.items.length ? (done / m.items.length) * 100 : 0}%`,
          }}
        />
      </div>
    </button>
  );
}
function Modal({
  children,
  title,
  onClose,
  wide = false,
}: {
  children: React.ReactNode;
  title: string;
  onClose: () => void;
  wide?: boolean;
}) {
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const previous = document.activeElement as HTMLElement | null;
    const old = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    ref.current?.focus();
    return () => {
      document.body.style.overflow = old;
      previous?.focus();
    };
  }, []);
  return (
    <div
      className="modal-backdrop"
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div
        ref={ref}
        tabIndex={-1}
        role="dialog"
        aria-modal="true"
        aria-label={title}
        className={`modal ${wide ? "wide" : ""}`}
        onKeyDown={(e) => {
          if (e.key === "Tab") {
            const els = ref.current?.querySelectorAll<HTMLElement>(
              'button:not([disabled]),input,textarea,select,[tabindex="0"]',
            );
            if (!els?.length) return;
            const first = els[0],
              last = els[els.length - 1];
            if (
              e.shiftKey &&
              (document.activeElement === first ||
                document.activeElement === ref.current)
            ) {
              e.preventDefault();
              last.focus();
            } else if (!e.shiftKey && document.activeElement === last) {
              e.preventDefault();
              first.focus();
            }
          }
        }}
      >
        <div className="modal-heading">
          <h2>{title}</h2>
          <button className="icon-button" onClick={onClose} aria-label="关闭">
            <X size={20} />
          </button>
        </div>
        {children}
      </div>
    </div>
  );
}
function CreateDialog({
  draft,
  memoryCount,
  models,
  onClose,
  onSaved,
}: {
  draft: string;
  memoryCount: number;
  models: ModelOverview;
  onClose: () => void;
  onSaved: (id: string, warning?: string) => Promise<void>;
}) {
  const [title, setTitle] = useState(draft.slice(0, 60));
  const [modelID, setModelID] = useState(models.default_id || (models.server_available ? "__server__" : ""));
  const [source, setSource] = useState(draft);
  const [category, setCategory] = useState("life");
  const [deadline, setDeadline] = useState("");
  const [reminder, setReminder] = useState(false);
  const [at, setAt] = useState(localInput());
  const [quiet, setQuiet] = useState(true);
  const [repeat, setRepeat] = useState("once");
  const [until, setUntil] = useState(
    localInput(new Date(Date.now() + 30 * 86400000).toISOString()).slice(0, 10),
  );
  const [useMemory, setUseMemory] = useState(true);
  const [confirmed, setConfirmed] = useState(false);
  const [generate, setGenerate] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const command = useRef<{ body: string; key: string } | null>(null);
  async function submit(e: React.FormEvent) {
    e.preventDefault();
    if (!confirmed) return;
    if (generate && !modelID) { setError("请先配置模型与 API Key，或取消自动整理后仅保存事项。"); return; }
    setBusy(true);
    setError("");
    try {
      const body = {
        title,
        source,
        category,
        deadline: deadline ? isoChina(deadline) : null,
        reminder_at: reminder ? isoChina(at) : null,
        quiet,
        repeat: reminder ? repeat : "once",
        repeat_until:
          reminder && repeat !== "once" && until ? endOfChinaDate(until) : null,
        timezone: "Asia/Shanghai",
        confirmed,
      };
      const serialized = JSON.stringify(body);
      if (command.current?.body !== serialized)
        command.current = { body: serialized, key: crypto.randomUUID() };
      const m = await api<Matter>(
        "/matters",
        body,
        "POST",
        command.current.key,
      );
      let warning = "";
      if (generate) {
        try {
          await api(
            `/matters/${m.id}/runs`,
            {
              expected_revision: m.revision,
              use_memory: useMemory,
              model_id: modelID,
            },
            "POST",
            `${command.current.key}-plan`,
          );
        } catch (e) {
          warning = `事项已保存，模型整理未启动：${(e as Error).message}`;
        }
      }
      await onSaved(m.id, warning);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  }
  return (
    <Modal title="记一件事" onClose={busy ? () => {} : onClose}>
      <p className="modal-intro">先确认目标与时间，妮米再帮你整理下一步。</p>
      <form onSubmit={(e) => void submit(e)} className="matter-form">
        <label>
          事项名称
          <input
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            placeholder="例如：准备周末的杭州之行"
            maxLength={100}
            required
          />
        </label>
        <label>
          补充资料
          <textarea
            value={source}
            onChange={(e) => setSource(e.target.value)}
            placeholder="粘贴材料要求、行程安排，或者你已经知道的信息…"
            rows={3}
            maxLength={6000}
          />
        </label>
        <div className="form-grid">
          <label>
            事项类型
            <select
              value={category}
              onChange={(e) => setCategory(e.target.value)}
            >
              {Object.entries(categories).map(([id, label]) => (
                <option value={id} key={id}>
                  {label}
                </option>
              ))}
            </select>
          </label>
          <label>
            截止时间（选填）
            <input
              type="datetime-local"
              value={deadline}
              onChange={(e) => {
                setDeadline(e.target.value);
                setConfirmed(false);
              }}
            />
          </label>
        </div>
        <label className="check-label">
          <input
            type="checkbox"
            checked={reminder}
            onChange={(e) => {
              setReminder(e.target.checked);
              setConfirmed(false);
            }}
          />
          设置提醒
        </label>
        {reminder && (
          <div className="reminder-settings">
            <label>
              提醒时间 · 北京时间
              <input
                type="datetime-local"
                value={at}
                onChange={(e) => {
                  setAt(e.target.value);
                  setConfirmed(false);
                }}
                required
              />
            </label>
            <RepeatFields
              repeat={repeat}
              until={until}
              onRepeat={(value) => {
                setRepeat(value);
                setConfirmed(false);
              }}
              onUntil={(value) => {
                setUntil(value);
                setConfirmed(false);
              }}
            />
            <label className="check-label">
              <input
                type="checkbox"
                checked={quiet}
                onChange={(e) => {
                  setQuiet(e.target.checked);
                  setConfirmed(false);
                }}
              />
              22:00–08:00 免打扰
            </label>
            <p>
              <Clock3 size={14} />
              预计记入站内：{quietPreview(at, quiet)}
            </p>
            <small>请打开页面查看提醒。微信与手机系统通知尚未接入。</small>
          </div>
        )}
        <label className="check-label">
          <input
            type="checkbox"
            checked={generate}
            onChange={(e) => setGenerate(e.target.checked)}
          />
          保存后整理一份行动清单
        </label>
        {generate && (
          <ModelChoice
            data={models}
            value={modelID}
            onChange={setModelID}
            disabled={busy}
          />
        )}
        {memoryCount > 0 && generate && (
          <label className="check-label">
            <input
              type="checkbox"
              checked={useMemory}
              onChange={(e) => setUseMemory(e.target.checked)}
            />
            参考已确认的偏好
          </label>
        )}
        <label className="check-label confirmation">
          <input
            type="checkbox"
            checked={confirmed}
            onChange={(e) => setConfirmed(e.target.checked)}
            required
          />
          我已确认事项内容与时间（北京时间）
        </label>
        {error && (
          <p className="form-error" role="alert">
            {error}
          </p>
        )}
        <div className="modal-actions">
          <button
            type="button"
            className="secondary"
            onClick={onClose}
            disabled={busy}
          >
            取消
          </button>
          <button
            type="submit"
            className="primary"
            disabled={busy || !confirmed}
          >
            {busy ? (
              <Busy />
            ) : (
              <>
                确认并保存
                <Check size={16} />
              </>
            )}
          </button>
        </div>
      </form>
    </Modal>
  );
}
function MatterDialog({
  matter: m,
  data,
  models,
  refresh,
  onClose,
}: {
  matter: Matter;
  data: Dashboard;
  models: ModelOverview;
  refresh: () => Promise<void>;
  onClose: () => void;
}) {
  const run = data.runs.find((r) => r.matter_id === m.id);
  const reminder = data.reminders.find((r) => r.matter_id === m.id);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [editingReminder, setEditingReminder] = useState(false);
  const [editingSource, setEditingSource] = useState(false);
  const [useMemory, setUseMemory] = useState(true);
  const [modelID, setModelID] = useState(models.default_id || (models.server_available ? "__server__" : ""));
  const [optimisticItems, setOptimisticItems] = useState<Item[] | null>(null);
  const shownItems = optimisticItems || m.items;
  async function mutate(body: unknown) {
    setBusy(true);
    setError("");
    try {
      await api(`/matters/${m.id}`, body, "PATCH");
      await refresh();
    } catch (err) {
      setError((err as Error).message);
      await refresh();
    } finally {
      setBusy(false);
    }
  }
  async function toggleItem(index: number) {
    const items = shownItems.map((x, i) =>
      i === index ? { ...x, done: !x.done } : x,
    );
    setOptimisticItems(items);
    try {
      await mutate({ expected_revision: m.revision, items });
    } finally {
      setOptimisticItems(null);
    }
  }
  async function generate() {
    setBusy(true);
    setError("");
    try {
      await api(`/matters/${m.id}/runs`, {
        expected_revision: m.revision,
        use_memory: useMemory,
        model_id: modelID,
      });
      await refresh();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  }
  function download() {
    const text = `# ${m.title}\n\n${m.items.map((i) => `- [${i.done ? "x" : " "}] ${i.text}`).join("\n")}\n\n${run?.result?.summary || ""}\n\n来源：用户提供资料${run?.used_memory_count ? "及已确认偏好" : ""}。${run?.result ? "AI 生成。" : "用户清单。"}未进行外部核验。\n`;
    const url = URL.createObjectURL(
      new Blob([text], { type: "text/markdown;charset=utf-8" }),
    );
    const a = document.createElement("a");
    a.href = url;
    a.download = "nemi-checklist.md";
    a.click();
    URL.revokeObjectURL(url);
  }
  return (
    <Modal title="事项详情" onClose={onClose} wide>
      <div className="detail-title">
        <span className="category">{categories[m.category]}</span>
        <h2>{m.title}</h2>
        <span className="status-badge">
          {m.status === "COMPLETED" ? "已完成" : "在跟进"}
        </span>
      </div>
      {error && (
        <p className="form-error" role="alert">
          {error}
        </p>
      )}
      {m.deadline && (
        <p className="detail-meta">
          <Clock3 size={16} />
          截止：{formatTime(m.deadline)}
        </p>
      )}
      {m.source && (
        <section className="source-block">
          <h3>
            <FileText size={16} />
            你提供的资料
          </h3>
          <p>{m.source}</p>
        </section>
      )}
      <button
        className="text-button"
        disabled={busy || m.status === "COMPLETED"}
        onClick={() => setEditingSource((value) => !value)}
      >
        <FileText size={15} />
        {editingSource ? "收起资料修改" : "补充或修改资料"}
      </button>
      {editingSource && (
        <SourceEditor
          matter={m}
          onCancel={() => setEditingSource(false)}
          onSaved={async () => {
            await refresh();
            setEditingSource(false);
          }}
        />
      )}
      <section className="checklist-section">
        <div className="detail-section-heading">
          <h3>行动清单</h3>
          <span>
            {shownItems.filter((i) => i.done).length} / {shownItems.length}
          </span>
        </div>
        {run?.result && <p className="plan-summary">{run.result.summary}</p>}
        {shownItems.map((item, index) => (
          <label
            className={`checklist-item ${item.done ? "done" : ""}`}
            key={`${index}-${item.text}`}
          >
            <input
              type="checkbox"
              checked={item.done}
              disabled={busy || m.status === "COMPLETED"}
              onChange={() => void toggleItem(index)}
            />
            <span>{item.text}</span>
          </label>
        ))}
        {!m.items.length && (
          <p className="muted">
            {run && ["QUEUED", "RUNNING"].includes(run.status)
              ? "妮米正在后台整理，关闭页面后也会继续。"
              : "让妮米根据你提供的内容整理准备步骤。"}
          </p>
        )}
        {run && (
          <p
            className={`run-status ${run.status === "FAILED" ? "form-error" : ""}`}
          >
            {["QUEUED", "RUNNING"].includes(run.status) && <Busy />}
            {runLabels[run.status]}
            {run.status === "FAILED" &&
              "，请检查服务配置；不确定的付费请求不会自动重发。"}
          </p>
        )}
        {!!run?.used_memory_count && (
          <p className="memory-used">
            <Brain size={14} />
            本次整理参考了 {run.used_memory_count} 条已确认偏好
          </p>
        )}
        {data.memories.length > 0 && m.status === "ACTIVE" && (
          <label className="check-label">
            <input
              type="checkbox"
              checked={useMemory}
              onChange={(e) => setUseMemory(e.target.checked)}
            />
            参考已确认的偏好
          </label>
        )}
        <div className="source-disclaimer">
          来源为所选资料与已确认偏好，未进行预约或其他外部操作。
          {m.origin_provider === "feishu" && m.origin_url && (
            <a
              className="text-button"
              href={m.origin_url}
              target="_blank"
              rel="noopener noreferrer"
            >
              查看飞书原文
            </a>
          )}
        </div>
        {m.status === "ACTIVE" && (
          <div className="matter-form detail-model-choice">
            <ModelChoice
              data={models}
              value={modelID}
              onChange={setModelID}
              disabled={
                busy || (!!run && ["QUEUED", "RUNNING"].includes(run.status))
              }
            />
          </div>
        )}
        <div className="detail-buttons">
          <button
            className="secondary"
            disabled={
              busy ||
              !modelID ||
              m.status === "COMPLETED" ||
              (!!run && ["QUEUED", "RUNNING"].includes(run.status))
            }
            onClick={() => void generate()}
          >
            <ListTodo size={15} />
            {m.items.length ? "重新整理清单" : "帮我整理清单"}
          </button>
          {m.items.length > 0 && (
            <button className="text-button" onClick={download}>
              <ArrowDownToLine size={15} />
              下载清单
            </button>
          )}
        </div>
      </section>
      <section className="detail-reminder">
        <div>
          <h3>
            <Bell size={16} />
            提醒
          </h3>
          <p>
            {reminder?.enabled
              ? `${formatTime(reminder.due_at)} · ${repeatLabels[reminder.repeat]} · ${reminder.sync_status === "FIRED" ? "已记入站内" : reminder.sync_status === "APPLIED" ? "已安排" : "正在同步"}`
              : "暂未设置提醒"}
          </p>
          {reminder?.enabled && reminder.repeat_until && (
            <small>
              结束日期：{localInput(reminder.repeat_until).slice(0, 10)} ·
              可以随时停用
            </small>
          )}
        </div>
        <button
          className="text-button"
          disabled={m.status === "COMPLETED"}
          onClick={() => setEditingReminder((v) => !v)}
        >
          {editingReminder ? "收起" : "设置"}
          <ChevronRight size={14} />
        </button>
      </section>
      {editingReminder && (
        <ReminderEditor
          matter={m}
          reminder={reminder}
          onSaved={async () => {
            await refresh();
            setEditingReminder(false);
          }}
        />
      )}
      <MatterApplications
        matter={m}
        calendarAvailable={
          m.status === "ACTIVE" && (!!m.deadline || !!reminder?.enabled)
        }
      />
      <div className="modal-actions">
        <button className="secondary" onClick={onClose}>
          关闭
        </button>
        <button
          className="primary"
          disabled={busy}
          onClick={() =>
            void mutate({
              expected_revision: m.revision,
              status: m.status === "ACTIVE" ? "COMPLETED" : "ACTIVE",
            })
          }
        >
          {busy ? (
            <Busy />
          ) : (
            <>
              <CheckCheck size={16} />
              {m.status === "ACTIVE" ? "标记已完成" : "重新跟进"}
            </>
          )}
        </button>
      </div>
    </Modal>
  );
}
function ReminderEditor({
  matter,
  reminder,
  onSaved,
}: {
  matter: Matter;
  reminder?: Reminder;
  onSaved: () => Promise<void>;
}) {
  const [at, setAt] = useState(localInput(reminder?.nominal_at));
  const [quiet, setQuiet] = useState(reminder?.quiet ?? true);
  const [repeat, setRepeat] = useState<string>(reminder?.repeat || "once");
  const [until, setUntil] = useState(
    localInput(
      reminder?.repeat_until ||
        new Date(Date.now() + 30 * 86400000).toISOString(),
    ).slice(0, 10),
  );
  const [enabled, setEnabled] = useState(true);
  const [confirmed, setConfirmed] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const command = useRef<{ body: string; key: string } | null>(null);
  const [expected] = useState(reminder?.revision || 0);
  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      const body = {
        expected_revision: expected,
        at: isoChina(at),
        quiet,
        enabled,
        confirmed,
        timezone: "Asia/Shanghai",
        repeat,
        repeat_until: repeat !== "once" && until ? endOfChinaDate(until) : null,
      };
      const serialized = JSON.stringify(body);
      if (command.current?.body !== serialized)
        command.current = { body: serialized, key: crypto.randomUUID() };
      await api(
        `/matters/${matter.id}/reminder`,
        body,
        "PUT",
        command.current.key,
      );
      await onSaved();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  }
  return (
    <form className="reminder-settings" onSubmit={(e) => void submit(e)}>
      <label>
        提醒时间 · 北京时间
        <input
          type="datetime-local"
          value={at}
          required
          onChange={(e) => {
            setAt(e.target.value);
            setConfirmed(false);
          }}
        />
      </label>
      <RepeatFields
        repeat={repeat}
        until={until}
        disabled={!enabled}
        onRepeat={(value) => {
          setRepeat(value);
          setConfirmed(false);
        }}
        onUntil={(value) => {
          setUntil(value);
          setConfirmed(false);
        }}
      />
      <label className="check-label">
        <input
          type="checkbox"
          checked={enabled}
          onChange={(e) => {
            setEnabled(e.target.checked);
            setConfirmed(false);
          }}
        />
        启用此提醒
      </label>
      <label className="check-label">
        <input
          type="checkbox"
          checked={quiet}
          onChange={(e) => {
            setQuiet(e.target.checked);
            setConfirmed(false);
          }}
        />
        22:00–08:00 免打扰
      </label>
      <p>
        预计记入站内：{enabled ? quietPreview(at, quiet) : "停用，不再触发"}
      </p>
      <label className="check-label">
        <input
          type="checkbox"
          checked={confirmed}
          onChange={(e) => setConfirmed(e.target.checked)}
          required
        />
        确认此次更改
      </label>
      {error && (
        <p className="form-error" role="alert">
          {error}
        </p>
      )}
      <button className="primary" disabled={busy || !confirmed}>
        {busy ? <Busy /> : "保存提醒"}
      </button>
    </form>
  );
}
