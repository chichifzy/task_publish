import { useCallback, useState, type FormEvent } from "react";
import { api, type AdminBlockInfo, type UserInfo } from "../api";
import { useAction, useLoader } from "../hooks";
import { roleLabel, userLabel } from "../labels";
import Messages from "../components/Messages";
import { useConfirm } from "../components/Confirm";

/** 管理员：授予或撤销高级用户身份。 */
export default function UsersPage() {
  const [query, setQuery] = useState("");
  const [applied, setApplied] = useState("");
  const fetchUsers = useCallback(() => api.listUsers(applied), [applied]);
  const { data: users, error: loadError, load } = useLoader(fetchUsers);
  const { run, error, notice } = useAction(load);

  const search = (e: FormEvent) => {
    e.preventDefault();
    setApplied(query.trim());
  };

  const grant = (u: UserInfo) => run(() => api.setRole(u.uid, "super"), `已将 ${userLabel(u)} 设为高级用户`);

  const confirm = useConfirm();

  const revoke = async (u: UserInfo) => {
    if (
      !(await confirm({
        title: "撤销高级用户",
        message:
          `撤销 ${userLabel(u)} 的高级用户身份？\n\n` +
          "· 私有仓库冻结，数据保留\n· 已派发的任务仍然有效\n· 与普通用户的绑定保留，但他不能再派发任务\n\n重新设为高级用户后私有仓库自动恢复。",
        confirmText: "撤销",
        danger: true,
      }))
    )
      return;
    run(() => api.setRole(u.uid, "member"), `已撤销 ${userLabel(u)} 的高级用户身份`);
  };

  return (
    <>
      <h2>用户管理</h2>
      <form className="row" onSubmit={search} style={{ marginBottom: 12 }}>
        <input
          className="grow"
          placeholder="按用户 ID 或用户名搜索"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
        />
        <button type="submit">搜索</button>
      </form>
      <Messages error={error || loadError} notice={notice} />
      {users?.length === 0 ? (
        <p className="muted">没有找到用户</p>
      ) : (
        <table>
          <thead>
            <tr>
              <th>用户 ID</th>
              <th>用户名</th>
              <th>角色</th>
              <th>通用积分</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {users?.map((u) => (
              <tr key={u.uid}>
                <td>{u.uid}</td>
                <td>{u.nickname}</td>
                <td>{roleLabel[u.role]}</td>
                <td>{u.points}</td>
                <td>
                  {u.role === "super" ? (
                    <button className="danger" onClick={() => revoke(u)}>
                      撤销高级用户
                    </button>
                  ) : (
                    <button onClick={() => grant(u)}>设为高级用户</button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      <Blacklist />
    </>
  );
}

/** 管理员：不可绑定名单。有解除申请的排在前面。 */
function Blacklist() {
  const { data: blocks, error: loadError, load } = useLoader(api.adminBlocks);
  const { run, error, notice } = useAction(load);

  const confirm = useConfirm();

  const remove = async (b: AdminBlockInfo) => {
    if (
      !(await confirm({
        title: "解除不可绑定",
        message: `解除 ${userLabel(b.a)} 与 ${userLabel(b.b)} 的不可绑定状态？解除后双方可以重新申请绑定。`,
        confirmText: "解除",
      }))
    )
      return;
    run(() => api.resolveBlock(b.a.uid, b.b.uid, true), "已解除不可绑定状态");
  };

  return (
    <>
      <h2>不可绑定名单</h2>
      <Messages error={error || loadError} notice={notice} />
      {blocks?.length === 0 ? (
        <p className="muted">名单为空。普通用户强制解绑后，双方账号会出现在这里。</p>
      ) : (
        <table>
          <thead>
            <tr>
              <th>账号对</th>
              <th>强制解绑方</th>
              <th>时间</th>
              <th>解除申请</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {blocks?.map((b) => (
              <tr key={`${b.a.uid}-${b.b.uid}`}>
                <td>
                  {userLabel(b.a)} ↔ {userLabel(b.b)}
                </td>
                <td>{b.blockedBy}</td>
                <td className="muted">{b.createdAt.slice(0, 10)}</td>
                <td>{b.requester ? `${b.requester} 申请解除` : <span className="muted">无</span>}</td>
                <td className="row">
                  <button onClick={() => remove(b)}>{b.requester ? "同意解除" : "解除"}</button>
                  {b.requester && (
                    <button
                      className="secondary"
                      onClick={() => run(() => api.resolveBlock(b.a.uid, b.b.uid, false), "已拒绝解除申请")}
                    >
                      拒绝
                    </button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </>
  );
}
