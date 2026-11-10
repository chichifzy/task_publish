import { useEffect, useImperativeHandle, useRef, type ReactNode, type Ref } from "react";
import { createPortal } from "react-dom";

export type ModalHandle = {
  /** 播放收回动画后调用 then（通常在 then 里卸载弹窗） */
  close: (then: () => void) => void;
};

type Props = {
  title: string;
  onClose: () => void;
  children: ReactNode;
  /** 追加到 dialog 上的修饰类，如 "confirm" */
  className?: string;
  ref?: Ref<ModalHandle>;
};

// 记录最近一次点击的按钮，作为弹窗飞出/收回的起点。
// 不用 document.activeElement：Safari 点击按钮不会让按钮获得焦点。
let lastTrigger: Element | null = null;
document.addEventListener(
  "pointerdown",
  (e) => {
    lastTrigger = (e.target as Element).closest?.("button") ?? null;
  },
  true,
);

const DURATION = 260;
const reducedMotion = () => window.matchMedia("(prefers-reduced-motion: reduce)").matches;

/** 弹窗相对起点按钮的变换：中心对齐到按钮中心，并缩放到按钮大小 */
function originTransform(dialog: HTMLElement, origin: Element | null) {
  if (!origin?.isConnected) return "scale(0.92)";
  // 先取消进行中的动画，否则量到的是变换后的位置（开发模式下 StrictMode 会让打开动画执行两次）
  dialog.getAnimations().forEach((a) => a.cancel());
  const o = origin.getBoundingClientRect();
  const d = dialog.getBoundingClientRect();
  const dx = o.left + o.width / 2 - (d.left + d.width / 2);
  const dy = o.top + o.height / 2 - (d.top + d.height / 2);
  return `translate(${dx}px, ${dy}px) scale(${o.width / d.width}, ${o.height / d.height})`;
}

/** 模态弹窗：挂载即打开，从触发按钮的位置飞出；Esc、点击遮罩、右上角关闭按钮或 ref.close() 时收回到按钮。 */
export default function Modal({ title, onClose, children, className, ref }: Props) {
  const dialogRef = useRef<HTMLDialogElement>(null);
  const originRef = useRef<Element | null>(lastTrigger);
  const closingRef = useRef(false);

  const close = (then: () => void) => {
    const dialog = dialogRef.current;
    if (closingRef.current) return;
    if (!dialog || reducedMotion()) return then();
    closingRef.current = true;
    const opts = { duration: DURATION * 0.85, easing: "cubic-bezier(0.4, 0, 1, 1)", fill: "forwards" as const };
    dialog.animate({ opacity: [1, 0] }, { ...opts, pseudoElement: "::backdrop" });
    dialog
      .animate(
        [
          { transform: "none", opacity: 1 },
          { opacity: 1, offset: 0.6 },
          { transform: originTransform(dialog, originRef.current), opacity: 0.3 },
        ],
        opts,
      )
      .finished.then(then, then);
  };

  const onCloseRef = useRef(onClose);
  onCloseRef.current = onClose;
  const requestClose = () => close(() => onCloseRef.current());
  const requestCloseRef = useRef(requestClose);
  requestCloseRef.current = requestClose;

  useImperativeHandle(ref, () => ({ close }));

  useEffect(() => {
    const dialog = dialogRef.current!;
    dialog.showModal();
    dialog.querySelector<HTMLElement>(".modal-body input, .modal-body textarea, .modal-body select")?.focus();
    document.body.classList.add("modal-open");
    if (!reducedMotion()) {
      const opts = { duration: DURATION, easing: "cubic-bezier(0.2, 0.8, 0.2, 1)" };
      dialog.animate({ opacity: [0, 1] }, { ...opts, pseudoElement: "::backdrop" });
      // 透明度在前 40% 就到 1，让飞行过程看得见
      dialog.animate(
        [
          { transform: originTransform(dialog, originRef.current), opacity: 0.3 },
          { opacity: 1, offset: 0.4 },
          { transform: "none", opacity: 1 },
        ],
        opts,
      );
    }
    const onCancel = (e: Event) => {
      e.preventDefault();
      requestCloseRef.current();
    };
    dialog.addEventListener("cancel", onCancel);
    return () => {
      dialog.removeEventListener("cancel", onCancel);
      document.body.classList.remove("modal-open");
    };
  }, []);

  // 渲染到 body 下，不插入页面内容，避免影响 :first-child 等布局选择器
  return createPortal(
    <dialog
      ref={dialogRef}
      className={className ? `modal ${className}` : "modal"}
      aria-label={title}
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) requestClose();
      }}
    >
      <div className="modal-panel">
        <div className="modal-head">
          <h3>{title}</h3>
          <button type="button" className="modal-close" aria-label="关闭" onClick={requestClose}>
            ×
          </button>
        </div>
        {children}
      </div>
    </dialog>,
    document.body,
  );
}
