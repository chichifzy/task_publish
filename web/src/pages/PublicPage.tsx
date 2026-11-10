import { useState, type FormEvent } from "react";
import { api, taskCode, type PublicTaskInput, type Task } from "../api";
import { useMe } from "../auth";
import { useAction, useLoader } from "../hooks";
import TaskCard, { type Tag } from "../components/TaskCard";
import Messages from "../components/Messages";
import DrawBox from "../components/DrawBox";

const emptyForm: PublicTaskInput = { description: "", points: 1, visibility: "all", enabled: true };

/** 公共任务仓库。管理员：增删改、复制；高级用户：查看、复制；普通用户：接取、随机抽取。 */
export default function PublicPage() {
  const { me, refresh } = useMe();
  const { data: tasks, error: loadError, load } = useLoader(api.listPublic);
  const { run, error, notice, setNotice } = useAction(() => {
    load();
    refresh();
  });
  const [editing, setEditing] = useState<Task | null>(null);
  const isAdmin = me.role === "admin";
  const isMember = me.role === "member";

  const tagsFor = (t: Task): Tag[] => {
    const tags: Tag[] = [];
    if (!isMember) {
      tags.push(
        t.visibility === "all"
          ? { text: "全体可见", tone: "blue" }
          : { text: "仅管理员和高级用户可见", tone: "orange" },
      );
    }
    if (isAdmin) {
      tags.push(t.enabled ? { text: "启用", tone: "green" } : { text: "禁用", tone: "red" });
      if (t.activeCount) tags.push({ text: `${t.activeCount} 人进行中` });
    }
    if (isMember && t.myActive) tags.push({ text: "我正在进行", tone: "blue" });
    return tags;
  };

  const remove = (t: Task) => {
    if (confirm(`确定删除 ${taskCode(t.id, true)}？已接取的任务不受影响。`)) {
      run(() => api.deletePublic(t.id), `已删除 ${taskCode(t.id, true)}`);
    }
  };

  const copy = (t: Task) =>
    run(async () => {
      const r = await api.copyPublic(t.id);
      setNotice(`已复制到私有仓库：${taskCode(r.id, false)}`);
    });

  return (
    <>
      {isAdmin && (
        <PublicTaskForm
          key={editing?.id ?? "new"}
          task={editing}
          onSaved={() => {
            setEditing(null);
            load();
          }}
          onCancel={() => setEditing(null)}
        />
      )}
      {isMember && (
        <DrawBox
          onAccepted={() => {
            load();
            refresh();
          }}
        />
      )}

      <h2>{isMember ? "可接取的任务" : "公共任务仓库"}</h2>
      <Messages error={error || loadError} notice={notice} />
      {tasks?.length === 0 && <p className="muted">暂无任务</p>}
      {tasks?.map((t) => (
        <TaskCard
          key={t.id}
          code={taskCode(t.id, true)}
          points={t.points}
          tags={tagsFor(t)}
          description={t.description}
          actions={
            <>
              {isMember && (
                <button
                  disabled={t.myActive}
                  onClick={() => run(() => api.claimPublic(t.id), `已接取 ${taskCode(t.id, true)}，可在“我的任务”中提交`)}
                >
                  {t.myActive ? "进行中" : "接取"}
                </button>
              )}
              {!isMember && (
                <button className="secondary" onClick={() => copy(t)}>
                  复制到私有仓库
                </button>
              )}
              {isAdmin && (
                <>
                  <button
                    className="secondary"
                    onClick={() => {
                      setEditing(t);
                      window.scrollTo({ top: 0, behavior: "smooth" });
                    }}
                  >
                    编辑
                  </button>
                  <button className="danger" onClick={() => remove(t)}>
                    删除
                  </button>
                </>
              )}
            </>
          }
        />
      ))}
    </>
  );
}

function PublicTaskForm({ task, onSaved, onCancel }: { task: Task | null; onSaved: () => void; onCancel: () => void }) {
  const [form, setForm] = useState<PublicTaskInput>(
    task
      ? {
          description: task.description,
          points: task.points,
          visibility: task.visibility === "privileged" ? "privileged" : "all",
          enabled: task.enabled,
        }
      : emptyForm,
  );
  const { run, error, notice } = useAction(onSaved);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const ok = await run(
      () => (task ? api.updatePublic(task.id, form) : api.createPublic(form)),
      task ? undefined : "已创建",
    );
    if (ok && !task) setForm(emptyForm);
  };

  return (
    <form className="card" onSubmit={submit}>
      <span className="code">{task ? `编辑 ${taskCode(task.id, true)}` : "新建公共任务"}</span>
      <label>
        任务描述
        <textarea
          rows={3}
          maxLength={5000}
          value={form.description}
          onChange={(e) => setForm({ ...form, description: e.target.value })}
          required
        />
      </label>
      <div className="row">
        <label>
          积分奖励
          <input
            type="number"
            min={1}
            step={1}
            value={Number.isNaN(form.points) ? "" : form.points}
            onChange={(e) => setForm({ ...form, points: e.target.valueAsNumber })}
            required
          />
        </label>
        <label>
          可见范围
          <select
            value={form.visibility}
            onChange={(e) => setForm({ ...form, visibility: e.target.value as PublicTaskInput["visibility"] })}
          >
            <option value="all">全体可见</option>
            <option value="privileged">仅管理员和高级用户可见</option>
          </select>
        </label>
        <label>
          状态
          <select value={form.enabled ? "1" : "0"} onChange={(e) => setForm({ ...form, enabled: e.target.value === "1" })}>
            <option value="1">启用</option>
            <option value="0">禁用</option>
          </select>
        </label>
      </div>
      <Messages error={error} notice={notice} />
      <div className="row">
        <button type="submit">{task ? "保存" : "创建"}</button>
        {task && (
          <button type="button" className="secondary" onClick={onCancel}>
            取消
          </button>
        )}
      </div>
    </form>
  );
}
