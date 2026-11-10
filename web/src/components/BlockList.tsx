import { api } from "../api";
import { useAction, useLoader } from "../hooks";
import { roleLabel, userLabel } from "../labels";
import Messages from "./Messages";

/** 与我处于不可绑定状态的账号，可向管理员申请解除。没有记录时不显示。 */
export default function BlockList() {
  const { data: blocks, error: loadError, load } = useLoader(api.myBlocks);
  const { run, error, notice } = useAction(load);

  if (!blocks?.length && !error && !loadError) return null;
  return (
    <>
      <h2>不可绑定</h2>
      <Messages error={error || loadError} notice={notice} />
      <p className="muted">因强制解绑，以下账号与你无法再绑定。可向管理员申请解除。</p>
      <table>
        <tbody>
          {blocks?.map((b) => (
            <tr key={b.uid}>
              <td>{b.uid}</td>
              <td>{b.nickname}</td>
              <td className="muted">
                {roleLabel[b.role]} · {b.byMe ? "你强制解绑" : "对方强制解绑"}
              </td>
              <td>
                <button
                  className="secondary"
                  disabled={b.pending}
                  onClick={() => run(() => api.requestUnblock(b.uid), `已向管理员申请解除与 ${userLabel(b)} 的不可绑定状态`)}
                >
                  {b.pending ? "已申请，等待管理员审核" : "申请解除"}
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </>
  );
}
