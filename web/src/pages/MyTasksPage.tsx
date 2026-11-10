import { useEffect, useState } from "react";
import { api, taskCode, type Assignment } from "../api";
import { useMe } from "../auth";
import { useAction, useLoader, type Run } from "../hooks";
import { sourceText, statusTag } from "../labels";
import TaskCard from "../components/TaskCard";
import Messages from "../components/Messages";
import { useConfirm } from "../components/Confirm";

const filters: { key: string; label: string; match: (a: Assignment) => boolean }[] = [
  { key: "active", label: "未完成", match: (a) => a.status === "in_progress" || a.status === "submitted" },
  { key: "completed", label: "已完成", match: (a) => a.status === "completed" },
  { key: "abandoned", label: "已放弃", match: (a) => a.status === "abandoned" },
  { key: "cancelled", label: "已取消", match: (a) => a.status === "cancelled" },
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

  // 加载列表时后端已标记新任务为已查看，刷新侧边栏让红点消失
  useEffect(() => {
    if (data && me.newTasks) refresh();
  }, [data, me.newTasks, refresh]);
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
        <AssignmentCard
          key={a.id}
          a={a}
          general={me.points}
          exclusive={me.exclusivePoints.find((e) => e.super.uid === a.reviewer.uid)?.points ?? 0}
          run={run}
        />
      ))}
    </>
  );
}

/**
 * 放弃扣分规则（与后端一致）：通用任务只扣通用积分，通用积分为负数时不能放弃。
 * 专有任务先扣派发者的专有积分到 0，再用通用积分抵扣到 0，仍不足的部分让专有积分变为负数；
 * 为负数的积分不参与抵扣，两种积分都为负数时不能放弃。
 */
function splitPenalty(a: Assignment, penalty: number, general: number, exclusive: number) {
  if (!a.exclusive) return general < 0 ? null : { general: penalty, exclusive: 0 };
  if (exclusive < 0 && general < 0) return null;
  if (exclusive < 0) return { general: penalty, exclusive: 0 };
  const g = Math.min(Math.max(general, 0), penalty - Math.min(exclusive, penalty));
  return { general: g, exclusive: penalty - g };
}

function AssignmentCard({ a, general, exclusive, run }: { a: Assignment; general: number; exclusive: number; run: Run }) {
  const [text, setText] = useState(a.submission);
  const code = taskCode(a.taskId, a.taskPublic);
  const penalty = Math.floor(a.points / 2);
  const active = a.status === "in_progress" || a.status === "submitted";
  const split = splitPenalty(a, penalty, general, exclusive);
  const blockedText = a.exclusive ? "专有积分和通用积分都为负数时不能放弃任务" : "通用积分为负数时不能放弃任务";
  const deductText = split
    ? [split.exclusive > 0 && `${split.exclusive} 专有积分`, split.general > 0 && `${split.general} 通用积分`]
        .filter(Boolean)
        .join("、")
    : "";

  const confirm = useConfirm();

  const abandon = async () => {
    const balance = a.exclusive ? `当前专有 ${exclusive}、通用 ${general}` : `当前通用 ${general}`;
    const msg = penalty > 0 ? `放弃 ${code} 将扣除 ${deductText}（${balance}），确定放弃吗？` : `确定放弃 ${code} 吗？`;
    if (await confirm({ title: "放弃任务", message: msg, confirmText: "放弃", danger: true }))
      run(() => api.abandon(a.id), penalty > 0 ? `已放弃 ${code}，扣除 ${deductText}` : `已放弃 ${code}`);
  };

  return (
    <TaskCard
      code={code}
      points={a.points}
      exclusive={a.exclusive}
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
              disabled={!split}
              title={split ? `放弃需扣除 ${deductText || "0 积分"}` : blockedText}
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
      {active && !split && <span className="muted">{blockedText}</span>}
    </TaskCard>
  );
}
