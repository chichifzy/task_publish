import { useRef, useState, type FormEvent } from "react";
import { api, taskCode, type PublicTaskInput, type Task } from "../api";
import { useMe } from "../auth";
import { useAction, useLoader } from "../hooks";
import TaskCard, { type Tag } from "../components/TaskCard";
import Messages from "../components/Messages";
import DrawBox from "../components/DrawBox";
import Modal, { type ModalHandle } from "../components/Modal";
import { useConfirm } from "../components/Confirm";

const emptyForm: PublicTaskInput = { description: "", points: 1, visibility: "all", enabled: true };

/** 公共任务仓库。管理员：增删改、复制；高级用户：查看、复制；普通用户：接取、随机抽取。 */
export default function PublicPage() {
  const { me, refresh } = useMe();
  const { data: tasks, error: loadError, load } = useLoader(api.listPublic);
  const { run, error, notice, setNotice } = useAction(() => {
    load();
    refresh();
  });
  // null：弹窗关闭；{ task: null }：新建；{ task }：编辑
  const [modal, setModal] = useState<{ task: Task | null } | null>(null);
  const confirm = useConfirm();
  const isAdmin = me.role === "admin";
  const isMember = me.role === "member";

  const tagsFor = (t: Task): Tag[] => {
    const tags: Tag[] = [];
    if (!isMember) {
      tags.push(
        t.visibility === "all"
          ? { text: "全体可见", tone: "blue" }
          : { text: "仅管理员和高级用户可见", tone: "orange" },
      );
    }
    if (isAdmin) {
      tags.push(t.enabled ? { text: "启用", tone: "green" } : { text: "禁用", tone: "red" });
      if (t.activeCount) tags.push({ text: `${t.activeCount} 人进行中` });
    }
    if (isMember && t.myActive) tags.push({ text: "我正在进行", tone: "blue" });
    return tags;
  };

  const remove = async (t: Task) => {
    if (
      await confirm({
        title: "删除任务",
        message: `确定删除 ${taskCode(t.id, true)}？已接取的任务不受影响。`,
        confirmText: "删除",
        danger: true,
      })
    ) {
      run(() => api.deletePublic(t.id), `已删除 ${taskCode(t.id, true)}`);
    }
  };

  const copy = (t: Task) =>
    run(async () => {
      const r = await api.copyPublic(t.id);
      setNotice(`已复制到私有仓库：${taskCode(r.id, false)}`);
    });

  return (
    <>
      {modal && (
        <PublicTaskForm
          task={modal.task}
          onSaved={(msg) => {
            setModal(null);
            setNotice(msg);
            load();
          }}
          onCancel={() => setModal(null)}
        />
      )}
      {isMember && (
        <DrawBox
          onAccepted={() => {
            load();
            refresh();
          }}
        />
      )}

      <div className="page-head">
        <h2>{isMember ? "可接取的任务" : "公共任务仓库"}</h2>
        {isAdmin && <button onClick={() => setModal({ task: null })}>新建公共任务</button>}
      </div>
      <Messages error={error || loadError} notice={notice} />
      {tasks?.length === 0 && <p className="muted">暂无任务</p>}
      {tasks?.map((t) => (
        <TaskCard
          key={t.id}
          code={taskCode(t.id, true)}
          points={t.points}
          tags={tagsFor(t)}
          description={t.description}
          actions={
            <>
              {isMember && (
                <button
                  disabled={t.myActive}
                  onClick={() => run(() => api.claimPublic(t.id), `已接取 ${taskCode(t.id, true)}，可在“我的任务”中提交`)}
                >
                  {t.myActive ? "进行中" : "接取"}
                </button>
              )}
              {!isMember && (
                <button className="secondary" onClick={() => copy(t)}>
                  复制到私有仓库
                </button>
              )}
              {isAdmin && (
                <>
                  <button className="secondary" onClick={() => setModal({ task: t })}>
                    编辑
                  </button>
                  <button className="danger" onClick={() => remove(t)}>
                    删除
                  </button>
                </>
              )}
            </>
          }
        />
      ))}
    </>
  );
}

function PublicTaskForm({
  task,
  onSaved,
  onCancel,
}: {
  task: Task | null;
  onSaved: (notice: string) => void;
  onCancel: () => void;
}) {
  const [form, setForm] = useState<PublicTaskInput>(
    task
      ? {
          description: task.description,
          points: task.points,
          visibility: task.visibility === "privileged" ? "privileged" : "all",
          enabled: task.enabled,
        }
      : emptyForm,
  );
  const { run, error, busy } = useAction(() => {});
  const modalRef = useRef<ModalHandle>(null);
  // 先播放收回动画，再通知父组件卸载弹窗
  const close = (then: () => void) => (modalRef.current ? modalRef.current.close(then) : then());

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const ok = await run(() => (task ? api.updatePublic(task.id, form) : api.createPublic(form)));
    if (ok) close(() => onSaved(task ? `已保存 ${taskCode(task.id, true)}` : "已创建"));
  };

  return (
    <Modal title={task ? `编辑 ${taskCode(task.id, true)}` : "新建公共任务"} onClose={onCancel} ref={modalRef}>
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
      <div className="row">
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
        <label>
          可见范围
          <select
            value={form.visibility}
            onChange={(e) => setForm({ ...form, visibility: e.target.value as PublicTaskInput["visibility"] })}
          >
            <option value="all">全体可见</option>
            <option value="privileged">仅管理员和高级用户可见</option>
          </select>
        </label>
        <label>
          状态
          <select value={form.enabled ? "1" : "0"} onChange={(e) => setForm({ ...form, enabled: e.target.value === "1" })}>
            <option value="1">启用</option>
            <option value="0">禁用</option>
          </select>
        </label>
      </div>
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
