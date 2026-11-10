import { useCallback, useState, type FormEvent } from "react";
import { api, type UserInfo } from "../api";
import { useAction, useLoader } from "../hooks";
import { roleLabel, userLabel } from "../labels";
import Messages from "../components/Messages";

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

  const revoke = (u: UserInfo) => {
    if (
      !confirm(
        `撤销 ${userLabel(u)} 的高级用户身份？\n\n` +
          "· 私有仓库冻结，数据保留\n· 已派发的任务仍然有效\n· 普通用户给他的授权保留，但他不能再派发任务\n\n重新设为高级用户后私有仓库自动恢复。",
      )
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
              <th>积分</th>
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
    </>
  );
}
