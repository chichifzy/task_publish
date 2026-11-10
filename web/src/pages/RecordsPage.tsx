import { api, taskCode, type Assignment } from "../api";
import { useMe } from "../auth";
import { useAction, useLoader } from "../hooks";
import { statusTag, userLabel } from "../labels";
import TaskCard from "../components/TaskCard";
import Messages from "../components/Messages";
import { useConfirm } from "../components/Confirm";

const done = (a: Assignment) => a.status === "completed" || a.status === "cancelled";
const active = (a: Assignment) => a.status === "in_progress" || a.status === "submitted";

/** 任务记录。普通用户：自己已完成/被取消的任务；高级用户和管理员：自己派发的任务，可取消未完成的。 */
export default function RecordsPage() {
  const { me } = useMe();
  const dispatcher = me.role !== "member";
  const { data, error: loadError, load } = useLoader(api.records);
  const { run, error, notice } = useAction(load);
  const confirm = useConfirm();

  const cancel = async (a: Assignment, code: string) => {
    if (
      await confirm({
        title: "取消任务",
        message: `取消派发给 ${userLabel(a.user)} 的 ${code}？对方不会获得积分，也不会被扣分。`,
        confirmText: "取消任务",
        danger: true,
      })
    )
      run(() => api.cancelAssignment(a.id), `已取消 ${code}`);
  };

  const remove = async (a: Assignment, code: string) => {
    if (await confirm({ title: "删除记录", message: `删除 ${code} 的这条记录？`, confirmText: "删除", danger: true }))
      run(() => api.deleteRecord(a.id), `已删除 ${code} 的记录`);
  };

  return (
    <>
      <h2>任务记录</h2>
      <Messages error={error || loadError} notice={notice} />
      {data?.length === 0 && <p className="muted">{dispatcher ? "还没有派发过任务" : "还没有已完成的任务"}</p>}
      {data?.map((a) => {
        const code = taskCode(a.taskId, a.taskPublic);
        return (
          <TaskCard
            key={a.id}
            code={code}
            points={a.points}
            exclusive={a.exclusive}
            tags={[
              statusTag[a.status],
              { text: dispatcher ? `派发给 ${userLabel(a.user)}` : a.source === "dispatch" ? `${userLabel(a.reviewer)} 派发` : "公共任务", tone: dispatcher ? "blue" : undefined },
            ]}
            description={a.description}
            actions={
              <>
                {dispatcher && active(a) && (
                  <button className="danger" onClick={() => cancel(a, code)}>
                    取消任务
                  </button>
                )}
                {done(a) && (
                  <button className="danger" onClick={() => remove(a, code)}>
                    删除
                  </button>
                )}
              </>
            }
          >
            {a.submission && (
              <p className="note">
                <b>完成情况：</b>
                {a.submission}
              </p>
            )}
            <span className="muted">
              派发于 {a.createdAt.slice(0, 10)} · 更新于 {a.updatedAt.slice(0, 10)}
            </span>
          </TaskCard>
        );
      })}
    </>
  );
}
