import { useCallback, useEffect, useState, type FormEvent } from "react";
import { api, type Task, type TaskInput } from "../api";

const empty: TaskInput = { title: "", description: "", status: "open" };

export default function AdminPage() {
  const [tasks, setTasks] = useState<Task[]>([]);
  const [form, setForm] = useState<TaskInput>(empty);
  const [editingId, setEditingId] = useState<number | null>(null);
  const [error, setError] = useState("");

  const load = useCallback(() => {
    api.listTasks().then(setTasks).catch((e: Error) => setError(e.message));
  }, []);
  useEffect(load, [load]);

  const run = async (fn: () => Promise<unknown>) => {
    setError("");
    try {
      await fn();
      load();
      return true;
    } catch (e) {
      setError((e as Error).message);
      return false;
    }
  };

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const ok = await run(() => (editingId === null ? api.createTask(form) : api.updateTask(editingId, form)));
    if (ok) {
      setForm(empty);
      setEditingId(null);
    }
  };

  const edit = (t: Task) => {
    setEditingId(t.id);
    setForm({ title: t.title, description: t.description, status: t.status });
  };

  const remove = (t: Task) => {
    if (confirm(`确定删除任务「${t.title}」？`)) run(() => api.deleteTask(t.id));
  };

  return (
    <>
      <form className="card" onSubmit={submit}>
        <h2>{editingId === null ? "新建任务" : `编辑任务 #${editingId}`}</h2>
        <label>
          标题
          <input value={form.title} onChange={(e) => setForm({ ...form, title: e.target.value })} required />
        </label>
        <label>
          描述
          <textarea
            rows={3}
            value={form.description}
            onChange={(e) => setForm({ ...form, description: e.target.value })}
          />
        </label>
        <label>
          状态
          <select
            value={form.status}
            onChange={(e) => setForm({ ...form, status: e.target.value as TaskInput["status"] })}
          >
            <option value="open">开放</option>
            <option value="closed">关闭</option>
          </select>
        </label>
        {error && <p className="error">{error}</p>}
        <div className="row">
          <button type="submit">{editingId === null ? "创建" : "保存"}</button>
          {editingId !== null && (
            <button
              type="button"
              className="secondary"
              onClick={() => {
                setEditingId(null);
                setForm(empty);
              }}
            >
              取消
            </button>
          )}
        </div>
      </form>

      <table>
        <thead>
          <tr>
            <th>ID</th>
            <th>标题</th>
            <th>状态</th>
            <th>接取人</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          {tasks.map((t) => (
            <tr key={t.id}>
              <td>{t.id}</td>
              <td>{t.title}</td>
              <td>{t.status === "open" ? "开放" : "关闭"}</td>
              <td>{t.claimedBy ?? "-"}</td>
              <td className="row">
                <button className="secondary" onClick={() => edit(t)}>
                  编辑
                </button>
                <button className="danger" onClick={() => remove(t)}>
                  删除
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </>
  );
}
