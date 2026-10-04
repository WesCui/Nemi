"use client";

import { useRef, useState } from "react";
import {
  Activity,
  ArrowRight,
  Brain,
  Check,
  Clock3,
  FileText,
  Pencil,
  Plus,
  Trash2,
} from "lucide-react";
import {
  api,
  Dashboard,
  formatTime,
  Matter,
  Memory,
  repeatLabels,
} from "@/lib/api";

const memoryCategories = {
  general: "通用偏好",
  life: "生活琐事",
  travel: "外出准备",
  work: "工作跟进",
};
export function RepeatFields({
  repeat,
  until,
  onRepeat,
  onUntil,
  disabled = false,
}: {
  repeat: string;
  until: string;
  onRepeat: (value: string) => void;
  onUntil: (value: string) => void;
  disabled?: boolean;
}) {
  return (
    <div className="form-grid repeat-fields">
      <label>
        重复频率
        <select
          value={repeat}
          onChange={(e) => onRepeat(e.target.value)}
          disabled={disabled}
        >
          {Object.entries(repeatLabels).map(([value, label]) => (
            <option key={value} value={value}>
              {label}
            </option>
          ))}
        </select>
      </label>
      {repeat !== "once" && (
        <label>
          结束日期 · 北京时间
          <input
            type="date"
            value={until}
            onChange={(e) => onUntil(e.target.value)}
            disabled={disabled}
            required={!disabled}
          />
        </label>
      )}
      {repeat === "weekdays" && (
        <small className="full-width">
          按周一至周五安排，暂不计算法定节假日和调休。
        </small>
      )}
      {repeat !== "once" && (
        <small className="full-width">
          到结束日期停止计划。免打扰可能将当天的提醒延后至次日 08:00。
        </small>
      )}
    </div>
  );
}

export function MemoryPanel({
  memories,
  refresh,
}: {
  memories: Memory[];
  refresh: () => Promise<void>;
}) {
  const [editing, setEditing] = useState<Memory | "new" | null>(null);
  const [removing, setRemoving] = useState<Memory | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  async function remove() {
    if (!removing) return;
    setBusy(true);
    setError("");
    try {
      await api(
        `/memories/${removing.id}`,
        { expected_revision: removing.revision, confirmed: true },
        "DELETE",
      );
      await refresh();
      setRemoving(null);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  return (
    <section className="memory-panel">
      <div className="section-heading">
        <div>
          <h2>妮米记住的偏好</h2>
          <p>只保存你确认的内容，生成清单时可以选择引用。</p>
        </div>
        <button
          className="primary"
          onClick={() => {
            setEditing("new");
            setError("");
          }}
          disabled={busy || memories.length >= 50}
        >
          <Plus size={16} />
          记住一个偏好
        </button>
      </div>
      <div className="notice">
        <Brain size={18} />
        <span>
          例如出行方式、材料整理习惯、沟通风格。请勿保存证件号码、密码或敏感健康资料。
        </span>
      </div>
      {editing && (
        <MemoryForm
          key={editing === "new" ? "new" : editing.id}
          memory={editing === "new" ? undefined : editing}
          onCancel={() => setEditing(null)}
          onSaved={async () => {
            await refresh();
            setEditing(null);
          }}
        />
      )}
      {error && (
        <p className="form-error" role="alert">
          {error}
        </p>
      )}
      <div className="memory-grid">
        {memories.map((memory) => (
          <article className="memory-card" key={memory.id}>
            <span className="category">
              <Brain size={15} />
              {memoryCategories[memory.category]}
            </span>
            <p>{memory.text}</p>
            <small>更新于 {formatTime(memory.updated_at)}</small>
            {removing?.id === memory.id ? (
              <div
                className="remove-confirm"
                role="group"
                aria-label="移除偏好确认"
              >
                <p>
                  移除后，新请求不再引用它。已提交的请求和已有结果不会撤回。
                </p>
                <button
                  className="secondary"
                  onClick={() => setRemoving(null)}
                  disabled={busy}
                >
                  保留
                </button>
                <button
                  className="danger-button"
                  onClick={() => void remove()}
                  disabled={busy}
                >
                  确认移除
                </button>
              </div>
            ) : (
              <div className="detail-buttons">
                <button
                  className="text-button"
                  onClick={() => setEditing(memory)}
                >
                  <Pencil size={14} />
                  修改
                </button>
                <button
                  className="text-button"
                  onClick={() => setRemoving(memory)}
                >
                  <Trash2 size={14} />
                  移除
                </button>
              </div>
            )}
          </article>
        ))}
      </div>
      {!memories.length && !editing && (
        <div className="empty-state">
          <Brain size={28} />
          <h3>先记住一个小习惯</h3>
          <p>比如“出行优先高铁”“清单按准备顺序排列”。你可以随时修改或移除。</p>
        </div>
      )}
    </section>
  );
}

function MemoryForm({
  memory,
  onCancel,
  onSaved,
}: {
  memory?: Memory;
  onCancel: () => void;
  onSaved: () => Promise<void>;
}) {
  const [text, setText] = useState(memory?.text || "");
  const [category, setCategory] = useState(memory?.category || "general");
  const [confirmed, setConfirmed] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const command = useRef<{ body: string; key: string } | null>(null);
  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      const body = {
        text,
        category,
        confirmed,
        expected_revision: memory?.revision || 0,
      };
      const serialized = JSON.stringify(body);
      if (command.current?.body !== serialized)
        command.current = { body: serialized, key: crypto.randomUUID() };
      await api(
        memory ? `/memories/${memory.id}` : "/memories",
        body,
        memory ? "PUT" : "POST",
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
    <form
      className="memory-form matter-form"
      onSubmit={(e) => void submit(e)}
      aria-label="保存偏好"
    >
      <h3>{memory ? "修改偏好" : "记住一个偏好"}</h3>
      <label>
        偏好内容
        <textarea
          value={text}
          onChange={(e) => {
            setText(e.target.value);
            setConfirmed(false);
          }}
          rows={3}
          maxLength={500}
          required
          placeholder="例如：周末出行优先高铁，行程不要太赶。"
        />
      </label>
      <label>
        适用场景
        <select
          value={category}
          onChange={(e) => {
            setCategory(e.target.value as Memory["category"]);
            setConfirmed(false);
          }}
        >
          {Object.entries(memoryCategories).map(([id, label]) => (
            <option key={id} value={id}>
              {label}
            </option>
          ))}
        </select>
      </label>
      <label className="check-label confirmation">
        <input
          type="checkbox"
          checked={confirmed}
          onChange={(e) => setConfirmed(e.target.checked)}
          required
        />
        我确认保存，用于后续同类清单
      </label>
      {error && (
        <p className="form-error" role="alert">
          {error}
        </p>
      )}
      <div className="detail-buttons">
        <button
          className="secondary"
          type="button"
          onClick={onCancel}
          disabled={busy}
        >
          取消
        </button>
        <button className="primary" disabled={busy || !confirmed}>
          <Check size={15} />
          {busy ? "正在保存…" : "保存偏好"}
        </button>
      </div>
    </form>
  );
}

export function SourceEditor({
  matter,
  onSaved,
  onCancel,
}: {
  matter: Matter;
  onSaved: () => Promise<void>;
  onCancel: () => void;
}) {
  const [title, setTitle] = useState(matter.title);
  const [source, setSource] = useState(matter.source);
  const [expected] = useState(matter.revision);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const command = useRef<{ body: string; key: string } | null>(null);
  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      const body = { title, source, expected_revision: expected };
      const serialized = JSON.stringify(body);
      if (command.current?.body !== serialized)
        command.current = { body: serialized, key: crypto.randomUUID() };
      await api(`/matters/${matter.id}`, body, "PATCH", command.current.key);
      await onSaved();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  }
  return (
    <form
      className="source-editor matter-form"
      onSubmit={(e) => void submit(e)}
      aria-label="更新事项资料"
    >
      <label>
        更新事项名称
        <input
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          required
          maxLength={100}
        />
      </label>
      <label>
        更新资料
        <textarea
          value={source}
          onChange={(e) => setSource(e.target.value)}
          maxLength={4000}
          rows={4}
        />
      </label>
      <small>
        保存资料不会覆盖现有清单；保存后可重新整理。之前启动的整理会保留结果，避免覆盖你刚修改的事项。
      </small>
      {error && (
        <p className="form-error" role="alert">
          {error}
        </p>
      )}
      <div className="detail-buttons">
        <button
          type="button"
          className="secondary"
          onClick={onCancel}
          disabled={busy}
        >
          取消修改
        </button>
        <button className="primary" disabled={busy}>
          {busy ? "正在保存…" : "保存资料"}
        </button>
      </div>
    </form>
  );
}

const eventLabels: Record<string, string> = {
  "matter.created": "事项已保存",
  "matter.updated": "事项已更新",
  "run.queued": "已安排清单整理",
  "run.running": "开始整理清单",
  "run.succeeded": "清单已生成",
  "run.failed": "整理未完成",
  "reminder.updated": "提醒计划已更新",
  "notification.available": "提醒已记入站内",
  "reminder.ended": "周期提醒已结束",
  "memory.saved": "偏好已记住",
  "memory.updated": "偏好已修改",
  "memory.deleted": "偏好已移除",
};
export function ActivityPanel({
  data,
  onSelect,
}: {
  data: Dashboard;
  onSelect: (id: string) => void;
}) {
  const running = data.runs.filter((r) =>
    ["QUEUED", "RUNNING"].includes(r.status),
  );
  return (
    <section className="activity-panel">
      <div className="section-heading">
        <div>
          <h2>妮米的工作动态</h2>
          <p>查看后台进度、计划变化和已经完成的结果。</p>
        </div>
        <Activity size={24} />
      </div>
      <div className="activity-summary">
        <span>
          <b>{running.length}</b> 正在整理
        </span>
        <span>
          <b>
            {
              data.reminders.filter(
                (r) => r.enabled && r.sync_status !== "FIRED",
              ).length
            }
          </b>{" "}
          待提醒
        </span>
      </div>
      {running.map((run) => {
        const matter = data.matters.find((m) => m.id === run.matter_id);
        return (
          <button
            className="reminder-row"
            key={run.id}
            onClick={() => onSelect(run.matter_id)}
          >
            <Clock3 size={18} />
            <div>
              <b>{matter?.title || "清单整理"}</b>
              <small>
                {run.status === "QUEUED" ? "排队中" : "后台处理中"} ·
                关闭网页后继续
              </small>
            </div>
            <ArrowRight size={17} />
          </button>
        );
      })}
      <ol className="activity-timeline">
        {data.activity
          .filter((event) => eventLabels[event.kind])
          .map((event) => {
            const run = data.runs.find((r) => r.id === event.subject_id);
            const reminder = data.reminders.find(
              (r) => r.id === event.subject_id,
            );
            const matter = data.matters.find(
              (m) =>
                m.id ===
                (run?.matter_id || reminder?.matter_id || event.subject_id),
            );
            return (
              <li key={event.sequence}>
                <span className="timeline-dot">
                  <FileText size={14} />
                </span>
                <div>
                  <b>{eventLabels[event.kind]}</b>
                  <p>
                    {matter?.title ||
                      (event.kind.startsWith("memory.")
                        ? "生活偏好"
                        : "后台事项")}
                  </p>
                  <small>{formatTime(event.created_at)}</small>
                </div>
                {matter && (
                  <button
                    className="text-button"
                    onClick={() => onSelect(matter.id)}
                  >
                    查看
                    <ArrowRight size={14} />
                  </button>
                )}
              </li>
            );
          })}
      </ol>
      {!data.activity.length && (
        <p className="muted">记下一件事后，进度会留在这里。</p>
      )}
    </section>
  );
}
