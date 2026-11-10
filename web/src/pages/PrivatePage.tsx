import { useRef, useState, type FormEvent } from "react";
import { api, taskCode, type DispatchResult, type PrivateTaskInput, type Task } from "../api";
import { useMe } from "../auth";
import { useAction, useLoader } from "../hooks";
import { userLabel } from "../labels";
import TaskCard from "../components/TaskCard";
import Messages from "../components/Messages";
import Modal, { type ModalHandle } from "../components/Modal";
import { useConfirm } from "../components/Confirm";

/** 私有任务仓库：增删改、派发。被撤销的高级用户只能查看（冻结）。 */
export default function PrivatePage() {
  const { me } = useMe();
  const { data, error: loadError, load } = useLoader(api.listPrivate);
  const { run, error, notice, setNotice } = useAction(load);
  // null：弹窗关闭；{ task: null }：新建；{ task }：编辑
  const [modal, setModal] = useState<{ task: Task | null } | null>(null);
  const [dispatching, setDispatching] = useState<number | null>(null);
  const confirm = useConfirm();
  const isAdmin = me.role === "admin";
  const frozen = data?.frozen ?? me.role === "member";

  const remove = async (t: Task) => {
    if (
      await confirm({
        title: "删除任务",
        message: `确定删除 ${taskCode(t.id, false)}？已派发的任务不受影响。`,
        confirmText: "删除",
        danger: true,
      })
    ) {
      run(() => api.deletePrivate(t.id), `已删除 ${taskCode(t.id, false)}`);
    }
  };

  return (
    <>
      {frozen && (
        <p className="card muted">
          你的高级用户身份已被撤销，私有仓库已冻结：只能查看，不能修改或派发。已派发的任务仍然有效，提交后仍由你审核。
        </p>
      )}
      {modal && !frozen && (
        <PrivateTaskForm
          task={modal.task}
          isAdmin={isAdmin}
          onSaved={(msg) => {
            setModal(null);
            setNotice(msg);
            load();
          }}
          onCancel={() => setModal(null)}
        />
      )}

      <div className="page-head">
        <h2>我的私有仓库</h2>
        {!frozen && <button onClick={() => setModal({ task: null })}>新建私有任务</button>}
      </div>
      <Messages error={error || loadError} notice={notice} />
      {data?.tasks.length === 0 && <p className="muted">私有仓库还没有任务，可以新建，或从公共仓库复制</p>}
      {data?.tasks.map((t) => (
        <TaskCard
          key={t.id}
          code={taskCode(t.id, false)}
          points={t.points}
          tags={t.copiedFrom ? [{ text: `复制自 ${taskCode(t.copiedFrom, true)}` }] : []}
          description={t.description}
          actions={
            !frozen && (
              <>
                <button onClick={() => setDispatching(dispatching === t.id ? null : t.id)}>
                  {dispatching === t.id ? "收起派发" : "派发"}
                </button>
                <button className="secondary" onClick={() => setModal({ task: t })}>
                  编辑
                </button>
                <button className="danger" onClick={() => remove(t)}>
                  删除
                </button>
              </>
            )
          }
        >
          {dispatching === t.id && <DispatchPanel taskId={t.id} isAdmin={isAdmin} />}
        </TaskCard>
      ))}
    </>
  );
}

function PrivateTaskForm({
  task,
  isAdmin,
  onSaved,
  onCancel,
}: {
  task: Task | null;
  isAdmin: boolean;
  onSaved: (notice: string) => void;
  onCancel: () => void;
}) {
  const [form, setForm] = useState<PrivateTaskInput>(
    task ? { description: task.description, points: task.points } : { description: "", points: 1 },
  );
  const { run, error, busy } = useAction(() => {});
  const modalRef = useRef<ModalHandle>(null);
  // 先播放收回动画，再通知父组件卸载弹窗
  const close = (then: () => void) => (modalRef.current ? modalRef.current.close(then) : then());

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const ok = await run(() => (task ? api.updatePrivate(task.id, form) : api.createPrivate(form)));
    if (ok) close(() => onSaved(task ? `已保存 ${taskCode(task.id, false)}` : "已创建"));
  };

  return (
    <Modal title={task ? `编辑 ${taskCode(task.id, false)}` : "新建私有任务"} onClose={onCancel} ref={modalRef}>
      <form className="modal-body" onSubmit={submit}>
      <label>
        任务描述
        <textarea
          rows={3}
          maxLength={5000}
          value={form.description}
          onChange={(e) => setForm({ ...form, description: e.target.value })}
          required
        />
      </label>
      <label>
        积分奖励
        <input
          type="number"
          min={1}
          step={1}
          value={Number.isNaN(form.points) ? "" : form.points}
          onChange={(e) => setForm({ ...form, points: e.target.valueAsNumber })}
          required
        />
      </label>
      <span className="muted">
        {isAdmin ? "积分为不小于 1 的整数" : "积分为 1-9 的整数；从公共仓库复制的任务可以保留原积分，修改积分时需改为 1-9"}
      </span>
      <Messages error={error} />
      <div className="modal-foot">
        <button type="button" className="secondary" onClick={() => close(onCancel)}>
          取消
        </button>
        <button type="submit" disabled={busy}>
          {task ? "保存" : "创建"}
        </button>
      </div>
      </form>
    </Modal>
  );
}

function DispatchPanel({ taskId, isAdmin }: { taskId: number; isAdmin: boolean }) {
  const { data: targets, error: loadError } = useLoader(api.dispatchTargets);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [filter, setFilter] = useState("");
  const [results, setResults] = useState<DispatchResult[] | null>(null);
  const { run, error, busy } = useAction(() => {});

  const f = filter.trim().toLowerCase();
  const shown = (targets ?? []).filter(
    (t) => !f || t.uid.toLowerCase().includes(f) || t.nickname.toLowerCase().includes(f),
  );

  const toggle = (uid: string) => {
    const s = new Set(selected);
    if (s.has(uid)) s.delete(uid);
    else s.add(uid);
    setSelected(s);
  };

  const dispatch = () =>
    run(async () => {
      setResults(await api.dispatch(taskId, [...selected]));
      setSelected(new Set());
    });

  return (
    <div className="card">
      <span className="muted">
        {isAdmin ? "以下普通用户开启了“接受管理员派发任务”" : "以下普通用户授权了你派发任务"}
      </span>
      {targets?.length === 0 ? (
        <span className="muted">暂无可派发的用户</span>
      ) : (
        <>
          <input placeholder="按用户 ID 或用户名筛选" value={filter} onChange={(e) => setFilter(e.target.value)} />
          <div className="checklist">
            {shown.map((t) => (
              <label className="inline" key={t.uid}>
                <input type="checkbox" checked={selected.has(t.uid)} onChange={() => toggle(t.uid)} />
                {userLabel(t)}
              </label>
            ))}
          </div>
          <div className="row">
            <button disabled={busy || selected.size === 0} onClick={dispatch}>
              派发给 {selected.size} 人
            </button>
          </div>
        </>
      )}
      {results?.map((r) => (
        <p key={r.uid} className={r.ok ? "success" : "error"}>
          {r.uid}：{r.ok ? "派发成功" : r.error}
        </p>
      ))}
      <Messages error={error || loadError} />
    </div>
  );
}
