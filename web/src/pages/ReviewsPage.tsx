import { useState } from "react";
import { api, taskCode, type Assignment } from "../api";
import { useMe } from "../auth";
import { useAction, useLoader, type Run } from "../hooks";
import { sourceText, userLabel } from "../labels";
import TaskCard from "../components/TaskCard";
import Messages from "../components/Messages";

/** 待我审核的提交。派发的任务由派发者审核，公共任务由管理员审核。 */
export default function ReviewsPage() {
  const { refresh } = useMe();
  const { data, error: loadError, load } = useLoader(api.reviews);
  const { run, error, notice } = useAction(() => {
    load();
    refresh();
  });

  return (
    <>
      <h2>待审核</h2>
      <Messages error={error || loadError} notice={notice} />
      {data?.length === 0 && <p className="muted">没有待审核的提交</p>}
      {data?.map((a) => (
        <ReviewCard key={a.id} a={a} run={run} />
      ))}
    </>
  );
}

function ReviewCard({ a, run }: { a: Assignment; run: Run }) {
  const [note, setNote] = useState("");
  const code = taskCode(a.taskId, a.taskPublic);
  return (
    <TaskCard
      code={code}
      points={a.points}
      tags={[{ text: userLabel(a.user), tone: "blue" }, { text: sourceText(a) }]}
      description={a.description}
      actions={
        <>
          <button onClick={() => run(() => api.review(a.id, true, note), `${code} 已通过，${userLabel(a.user)} 获得 ${a.points} 积分`)}>
            通过
          </button>
          <button
            className="danger"
            disabled={!note.trim()}
            title={note.trim() ? "" : "请先填写不通过的原因"}
            onClick={() => run(() => api.review(a.id, false, note), `${code} 已退回`)}
          >
            不通过
          </button>
        </>
      }
    >
      <p className="note">
        <b>完成情况：</b>
        {a.submission}
      </p>
      <textarea
        rows={2}
        maxLength={1000}
        placeholder="审核意见（不通过时必填）"
        value={note}
        onChange={(e) => setNote(e.target.value)}
      />
    </TaskCard>
  );
}
