import { useState, type FormEvent } from "react";
import { api, type BindStatus, type SuperInfo } from "../api";
import { useMe } from "../auth";
import { useAction, useLoader } from "../hooks";
import { userLabel } from "../labels";
import Messages from "../components/Messages";
import BlockList from "../components/BlockList";
import { useConfirm } from "../components/Confirm";

const statusText: Record<BindStatus, string> = {
  pending_bind: "等待对方同意绑定",
  bound: "已绑定",
  pending_unbind: "已申请解绑，等待对方同意",
};

/** 普通用户：管理员派发开关、绑定高级用户。 */
export default function SettingsPage() {
  const { me, refresh } = useMe();
  const { data: bindings, error: loadError, load } = useLoader(api.myBindings);
  const [blocksKey, setBlocksKey] = useState(0);
  const { run, error, notice } = useAction(() => {
    load();
    refresh();
    setBlocksKey((k) => k + 1);
  });
  const [query, setQuery] = useState("");
  const [results, setResults] = useState<SuperInfo[] | null>(null);
  const confirm = useConfirm();

  const search = (e: FormEvent) => {
    e.preventDefault();
    run(async () => setResults(await api.searchSupers(query)));
  };

  const toggleAdmin = (checked: boolean) =>
    run(
      () => api.updateSettings(checked),
      checked ? "已开启：管理员可以向你派发任务" : "已关闭：管理员不能向你派发任务",
    );

  const apply = (s: SuperInfo) =>
    run(async () => {
      await api.applyBind(s.uid);
      setResults((rs) => rs?.map((r) => (r.uid === s.uid ? { ...r, status: "pending_bind" } : r)) ?? rs);
    }, `已向 ${userLabel(s)} 发送绑定申请`);

  const act = async (uid: string, label: string, action: "cancel" | "unbind" | "force") => {
    if (
      action === "force" &&
      !(await confirm({
        title: "强制解绑",
        message: `强制解绑 ${label}？\n\n解绑立即生效，之后你们两个账号将无法再绑定（包括任意一方角色变化后）。\n如需恢复，可向管理员申请解除。`,
        confirmText: "强制解绑",
        danger: true,
      }))
    )
      return;
    const ok = { cancel: "已撤回申请", unbind: `已向 ${label} 申请解绑`, force: `已强制解绑 ${label}` }[action];
    run(async () => {
      await api.bindingAction(uid, action);
      setResults(null);
    }, ok);
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

      <h2>绑定高级用户</h2>
      <div className="card">
        <span className="muted">申请绑定后需对方同意。绑定的高级用户可以向你派发任务。</span>
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
                      {s.status === "" ? (
                        <button onClick={() => apply(s)}>申请绑定</button>
                      ) : (
                        <span className="muted">{s.status === "blocked" ? "不可绑定" : statusText[s.status]}</span>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          ))}
      </div>

      <h2>我的绑定</h2>
      {bindings?.length === 0 ? (
        <p className="muted">还没有绑定任何高级用户</p>
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
            {bindings?.map((b) => {
              const label = userLabel(b);
              return (
                <tr key={b.uid}>
                  <td>{b.uid}</td>
                  <td>{b.nickname}</td>
                  <td className={b.active ? "" : "muted"}>
                    {statusText[b.status]}
                    {!b.active && "（对方已被撤销高级用户身份，暂不能派发）"}
                  </td>
                  <td className="row">
                    {b.status === "bound" && (
                      <button className="secondary" onClick={() => act(b.uid, label, "unbind")}>
                        申请解绑
                      </button>
                    )}
                    {b.status !== "bound" && (
                      <button className="secondary" onClick={() => act(b.uid, label, "cancel")}>
                        撤回申请
                      </button>
                    )}
                    {b.status !== "pending_bind" && (
                      <button className="danger" onClick={() => act(b.uid, label, "force")}>
                        强制解绑
                      </button>
                    )}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      )}

      <BlockList key={blocksKey} />
    </>
  );
}
