import { useState } from "react";
import { api, taskCode, type Assignment } from "../api";
import { useMe } from "../auth";
import { useAction, useLoader, type Run } from "../hooks";
import { sourceText, statusTag } from "../labels";
import TaskCard from "../components/TaskCard";
import Messages from "../components/Messages";

const filters: { key: string; label: string; match: (a: Assignment) => boolean }[] = [
  { key: "active", label: "未完成", match: (a) => a.status === "in_progress" || a.status === "submitted" },
  { key: "completed", label: "已完成", match: (a) => a.status === "completed" },
  { key: "abandoned", label: "已放弃", match: (a) => a.status === "abandoned" },
  { key: "all", label: "全部", match: () => true },
];

/** 普通用户的任务：提交完成情况、放弃。 */
export default function MyTasksPage() {
  const { me, refresh } = useMe();
  const { data, error: loadError, load } = useLoader(api.myAssignments);
  const { run, error, notice } = useAction(() => {
    load();
    refresh();
  });
  const [filter, setFilter] = useState("active");
  const current = filters.find((f) => f.key === filter)!;
  const list = (data ?? []).filter(current.match);

  return (
    <>
      <h2>我的任务</h2>
      <div className="filters">
        {filters.map((f) => (
          <button key={f.key} className={f.key === filter ? "on" : ""} onClick={() => setFilter(f.key)}>
            {f.label}
          </button>
        ))}
      </div>
      <Messages error={error || loadError} notice={notice} />
      {data && list.length === 0 && <p className="muted">没有{current.label === "全部" ? "" : current.label}的任务</p>}
      {list.map((a) => (
        <AssignmentCard key={a.id} a={a} myPoints={me.points} run={run} />
      ))}
    </>
  );
}

function AssignmentCard({ a, myPoints, run }: { a: Assignment; myPoints: number; run: Run }) {
  const [text, setText] = useState(a.submission);
  const code = taskCode(a.taskId, a.taskPublic);
  const penalty = Math.floor(a.points / 2);
  const active = a.status === "in_progress" || a.status === "submitted";

  const abandon = () => {
    const msg =
      penalty > 0
        ? `放弃 ${code} 将扣除 ${penalty} 积分（当前 ${myPoints} 积分），确定放弃吗？`
        : `确定放弃 ${code} 吗？`;
    if (confirm(msg)) run(() => api.abandon(a.id), penalty > 0 ? `已放弃 ${code}，扣除 ${penalty} 积分` : `已放弃 ${code}`);
  };

  return (
    <TaskCard
      code={code}
      points={a.points}
      tags={[statusTag[a.status], { text: sourceText(a, true) }]}
      description={a.description}
      actions={
        active && (
          <>
            {a.status === "in_progress" && (
              <button
                disabled={!text.trim()}
                onClick={() => run(() => api.submit(a.id, text), `已提交 ${code}，等待审核`)}
              >
                提交
              </button>
            )}
            <button
              className="danger"
              disabled={myPoints < 0}
              title={myPoints < 0 ? "积分为负数时不能放弃任务" : `放弃需扣除 ${penalty} 积分`}
              onClick={abandon}
            >
              放弃（-{penalty}）
            </button>
          </>
        )
      }
    >
      {a.status === "in_progress" && a.reviewNote && (
        <p className="note error">
          <b>退回原因：</b>
          {a.reviewNote}
        </p>
      )}
      {a.status === "in_progress" ? (
        <label>
          完成情况
          <textarea rows={3} maxLength={5000} value={text} onChange={(e) => setText(e.target.value)} />
        </label>
      ) : (
        a.submission && (
          <p className="note">
            <b>完成情况：</b>
            {a.submission}
          </p>
        )
      )}
      {active && myPoints < 0 && <span className="muted">积分为负数时不能放弃任务</span>}
    </TaskCard>
  );
}
