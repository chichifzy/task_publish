import { useState, type FormEvent } from "react";
import { api, type SuperInfo } from "../api";
import { useMe } from "../auth";
import { useAction, useLoader } from "../hooks";
import { userLabel } from "../labels";
import Messages from "../components/Messages";

/** 普通用户：管理员派发开关、授权高级用户。 */
export default function SettingsPage() {
  const { me, refresh } = useMe();
  const { data: auths, error: loadError, load } = useLoader(api.authorizations);
  const { run, error, notice } = useAction(() => {
    load();
    refresh();
  });
  const [query, setQuery] = useState("");
  const [results, setResults] = useState<SuperInfo[] | null>(null);

  const markResult = (uid: string, authorized: boolean) =>
    setResults((rs) => rs?.map((r) => (r.uid === uid ? { ...r, authorized } : r)) ?? rs);

  const search = (e: FormEvent) => {
    e.preventDefault();
    run(async () => setResults(await api.searchSupers(query)));
  };

  const toggleAdmin = (checked: boolean) =>
    run(
      () => api.updateSettings(checked),
      checked ? "已开启：管理员可以向你派发任务" : "已关闭：管理员不能向你派发任务",
    );

  const authorize = (s: SuperInfo) =>
    run(async () => {
      await api.authorize(s.uid);
      markResult(s.uid, true);
    }, `已授权 ${userLabel(s)}`);

  const unauthorize = (uid: string, label: string) => {
    if (!confirm(`取消对 ${label} 的授权？取消后对方不能再向你派发任务，已派发的任务不受影响。`)) return;
    run(async () => {
      await api.unauthorize(uid);
      markResult(uid, false);
    }, `已取消对 ${label} 的授权`);
  };

  return (
    <>
      <Messages error={error || loadError} notice={notice} />

      <h2>管理员派发</h2>
      <div className="card">
        <label className="inline">
          <input type="checkbox" checked={me.acceptAdminDispatch} onChange={(e) => toggleAdmin(e.target.checked)} />
          接受管理员派发任务
        </label>
        <span className="muted">关闭时管理员不能向你派发任务，已派发的任务不受影响。</span>
      </div>

      <h2>授权高级用户</h2>
      <div className="card">
        <span className="muted">被授权的高级用户可以向你派发任务。每次只能授权一个高级用户。</span>
        <form className="row" onSubmit={search}>
          <input
            className="grow"
            placeholder="输入高级用户的用户 ID 或用户名"
            value={query}
            maxLength={32}
            onChange={(e) => setQuery(e.target.value)}
          />
          <button type="submit" disabled={!query.trim()}>
            搜索
          </button>
        </form>
        {results &&
          (results.length === 0 ? (
            <span className="muted">没有找到匹配的高级用户</span>
          ) : (
            <table>
              <tbody>
                {results.map((s) => (
                  <tr key={s.uid}>
                    <td>{s.uid}</td>
                    <td>{s.nickname}</td>
                    <td>
                      <button disabled={s.authorized} onClick={() => authorize(s)}>
                        {s.authorized ? "已授权" : "授权"}
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          ))}
      </div>

      <h2>已授权的高级用户</h2>
      {auths?.length === 0 ? (
        <p className="muted">还没有授权任何高级用户</p>
      ) : (
        <table>
          <thead>
            <tr>
              <th>用户 ID</th>
              <th>用户名</th>
              <th>状态</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {auths?.map((a) => (
              <tr key={a.uid}>
                <td>{a.uid}</td>
                <td>{a.nickname}</td>
                <td className={a.active ? "" : "muted"}>{a.active ? "可派发" : "已被撤销高级用户身份，暂不能派发"}</td>
                <td>
                  <button className="secondary" onClick={() => unauthorize(a.uid, userLabel(a))}>
                    取消授权
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </>
  );
}
