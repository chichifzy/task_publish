import { useEffect, useState } from "react";
import { api } from "../api";
import { useAction } from "../hooks";
import Messages from "./Messages";

/** 普通用户随机抽取隐藏任务：先只看到积分，接取后才能看到任务内容。 */
export default function DrawBox({ onAccepted }: { onAccepted: () => void }) {
  const [drawn, setDrawn] = useState<number | null>(null); // 抽到的任务积分
  const { run, error, notice, busy } = useAction(() => {});

  useEffect(() => {
    api
      .currentDraw()
      .then((d) => setDrawn(d?.points ?? null))
      .catch(() => {});
  }, []);

  const draw = () =>
    run(async () => {
      setDrawn((await api.draw()).points);
    });

  const accept = () =>
    run(async () => {
      try {
        await api.acceptDraw();
      } finally {
        setDrawn(null);
      }
      onAccepted();
    }, "已接取，任务内容可在“我的任务”中查看");

  const discard = () =>
    run(async () => {
      await api.discardDraw();
      setDrawn(null);
    });

  return (
    <div className="card draw-box">
      <strong>随机抽取</strong>
      <span className="muted">从隐藏任务中随机抽取一个，先看积分再决定是否接取</span>
      {drawn === null ? (
        <button onClick={draw} disabled={busy}>
          抽取任务
        </button>
      ) : (
        <>
          <div className="draw-points">{drawn} 积分</div>
          <span className="muted">接取后才能看到任务内容</span>
          <div className="row">
            <button onClick={accept} disabled={busy}>
              接取
            </button>
            <button className="secondary" onClick={draw} disabled={busy}>
              再抽一次
            </button>
            <button className="secondary" onClick={discard} disabled={busy}>
              不接取
            </button>
          </div>
        </>
      )}
      <Messages error={error} notice={notice} />
    </div>
  );
}
