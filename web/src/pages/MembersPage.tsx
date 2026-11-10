import { useEffect } from "react";
import { api, type MemberInfo } from "../api";
import { useMe } from "../auth";
import { useAction, useLoader } from "../hooks";
import { userLabel } from "../labels";
import Messages from "../components/Messages";
import BlockList from "../components/BlockList";
import { useConfirm } from "../components/Confirm";

const day = (s: string | null) => (s ? s.slice(0, 10) : "-");

/** 高级用户：处理绑定/解绑申请，查看和解绑已绑定的普通用户。 */
export default function MembersPage() {
  const { me, refresh } = useMe();
  const { data: members, error: loadError, load } = useLoader(api.members);
  const { run, error, notice } = useAction(load);
  const confirm = useConfirm();

  // 加载列表时后端已标记为已查看，刷新侧边栏让红点消失
  useEffect(() => {
    if (members && me.newBindRequests) refresh();
  }, [members, me.newBindRequests, refresh]);

  const act = async (m: MemberInfo, action: "approve" | "reject" | "unbind", ok: string) => {
    if (
      action === "unbind" &&
      !(await confirm({
        title: "解除绑定",
        message: `解除与 ${userLabel(m)} 的绑定？解除后不能再向对方派发任务，已派发的任务不受影响。`,
        confirmText: "解除绑定",
        danger: true,
      }))
    )
      return;
    run(() => api.memberAction(m.uid, action), ok);
  };

  const pending = members?.filter((m) => m.status !== "bound") ?? [];
  const bound = members?.filter((m) => m.status !== "pending_bind") ?? [];

  return (
    <>
      <Messages error={error || loadError} notice={notice} />

      {pending.length > 0 && (
        <>
          <h2>待处理申请</h2>
          <table>
            <tbody>
              {pending.map((m) => {
                const label = userLabel(m);
                const isBind = m.status === "pending_bind";
                return (
                  <tr key={m.uid}>
                    <td>{label}</td>
                    <td>{isBind ? "申请绑定" : "申请解绑"}</td>
                    <td className="muted">{day(m.appliedAt)}</td>
                    <td className="row">
                      <button onClick={() => act(m, "approve", isBind ? `已与 ${label} 绑定` : `已与 ${label} 解绑`)}>
                        同意
                      </button>
                      <button className="secondary" onClick={() => act(m, "reject", "已拒绝申请")}>
                        拒绝
                      </button>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </>
      )}

      <h2>已绑定的普通用户</h2>
      {members && bound.length === 0 ? (
        <p className="muted">还没有普通用户与你绑定。普通用户可以在“设置”中搜索你的用户 ID 申请绑定。</p>
      ) : (
        <table>
          <thead>
            <tr>
              <th>用户 ID</th>
              <th>用户名</th>
              <th>通用积分</th>
              <th>专有积分</th>
              <th>注册时间</th>
              <th>绑定时间</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {bound.map((m) => (
              <tr key={m.uid}>
                <td>{m.uid}</td>
                <td>{m.nickname}</td>
                <td>{m.points}</td>
                <td>{m.exclusivePoints}</td>
                <td>{day(m.createdAt)}</td>
                <td>{day(m.boundAt)}</td>
                <td>
                  <button className="danger" onClick={() => act(m, "unbind", `已解除与 ${userLabel(m)} 的绑定`)}>
                    解除绑定
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      <BlockList />
    </>
  );
}
