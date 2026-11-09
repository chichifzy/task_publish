import { useCallback, useEffect, useState } from "react";
import { api, type Task } from "../api";

export default function TasksPage() {
  const [tasks, setTasks] = useState<Task[]>([]);
  const [error, setError] = useState("");

  const load = useCallback(() => {
    api.listTasks().then(setTasks).catch((e: Error) => setError(e.message));
  }, []);
  useEffect(load, [load]);

  const claim = async (id: number) => {
    setError("");
    try {
      await api.claimTask(id);
    } catch (e) {
      setError((e as Error).message);
    }
    load();
  };

  const mine = tasks.filter((t) => t.claimedByMe);
  const available = tasks.filter((t) => t.status === "open" && !t.claimed);

  return (
    <>
      {error && <p className="error">{error}</p>}
      <h2>可接取的任务</h2>
      {available.length === 0 && <p className="muted">暂无可接取的任务</p>}
      {available.map((t) => (
        <div className="card" key={t.id}>
          <h3>{t.title}</h3>
          {t.description && <p>{t.description}</p>}
          <button onClick={() => claim(t.id)}>接取</button>
        </div>
      ))}

      <h2>我接取的任务</h2>
      {mine.length === 0 && <p className="muted">还没有接取任务</p>}
      {mine.map((t) => (
        <div className="card" key={t.id}>
          <h3>{t.title}</h3>
          {t.description && <p>{t.description}</p>}
        </div>
      ))}
    </>
  );
}
