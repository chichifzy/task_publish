import { createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode } from "react";
import Modal, { type ModalHandle } from "./Modal";

export type ConfirmOptions = {
  title?: string;
  message: ReactNode;
  confirmText?: string;
  danger?: boolean;
};

type ConfirmFn = (opts: ConfirmOptions) => Promise<boolean>;

const ConfirmContext = createContext<ConfirmFn | null>(null);

type Pending = ConfirmOptions & { id: number; resolve: (ok: boolean) => void };

/** 提供应用内确认弹窗，替代浏览器原生 confirm()。 */
export function ConfirmProvider({ children }: { children: ReactNode }) {
  const [pending, setPending] = useState<Pending | null>(null);
  const idRef = useRef(0);

  const confirm = useCallback<ConfirmFn>(
    (opts) =>
      new Promise<boolean>((resolve) => {
        setPending((prev) => {
          prev?.resolve(false);
          return { ...opts, id: ++idRef.current, resolve };
        });
      }),
    [],
  );

  const finish = (ok: boolean) => {
    pending?.resolve(ok);
    setPending(null);
  };

  return (
    <ConfirmContext.Provider value={confirm}>
      {children}
      {pending && <ConfirmDialog key={pending.id} opts={pending} onDone={finish} />}
    </ConfirmContext.Provider>
  );
}

/** 返回 confirm(opts)：确认时 resolve(true)，取消/Esc/点遮罩/× 时 resolve(false)。 */
export function useConfirm() {
  const ctx = useContext(ConfirmContext);
  if (!ctx) throw new Error("useConfirm 必须在 ConfirmProvider 内使用");
  return ctx;
}

function ConfirmDialog({ opts, onDone }: { opts: ConfirmOptions; onDone: (ok: boolean) => void }) {
  const modalRef = useRef<ModalHandle>(null);
  const cancelRef = useRef<HTMLButtonElement>(null);
  const close = (ok: boolean) => (modalRef.current ? modalRef.current.close(() => onDone(ok)) : onDone(ok));

  // Modal 的 effect（showModal）先执行，这里再把焦点放到“取消”，避免回车误触危险操作
  useEffect(() => cancelRef.current?.focus(), []);

  return (
    <Modal title={opts.title ?? "确认"} className="confirm" onClose={() => onDone(false)} ref={modalRef}>
      <div className="modal-body">
        <p>{opts.message}</p>
        <div className="modal-foot">
          <button type="button" className="secondary" ref={cancelRef} onClick={() => close(false)}>
            取消
          </button>
          <button type="button" className={opts.danger ? "danger" : undefined} onClick={() => close(true)}>
            {opts.confirmText ?? "确定"}
          </button>
        </div>
      </div>
    </Modal>
  );
}
