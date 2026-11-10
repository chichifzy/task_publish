import { useCallback, useEffect, useState } from "react";
import { errMsg } from "./api";

/** 加载数据。fn 变化时重新加载，fn 需保持引用稳定（模块级函数或 useCallback）。 */
export function useLoader<T>(fn: () => Promise<T>) {
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState("");
  const load = useCallback(() => {
    fn()
      .then((d) => {
        setData(d);
        setError("");
      })
      .catch((e) => setError(errMsg(e)));
  }, [fn]);
  useEffect(() => {
    load();
  }, [load]);
  return { data, error, load };
}

/** 执行一个操作：成功后显示提示并调用 after，失败时显示错误。 */
export function useAction(after: () => void) {
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [busy, setBusy] = useState(false);
  const run = async (fn: () => Promise<unknown>, ok?: string) => {
    setError("");
    setNotice("");
    setBusy(true);
    try {
      await fn();
      if (ok) setNotice(ok);
      after();
      return true;
    } catch (e) {
      setError(errMsg(e));
      return false;
    } finally {
      setBusy(false);
    }
  };
  return { run, error, notice, busy, setNotice };
}

export type Run = ReturnType<typeof useAction>["run"];
