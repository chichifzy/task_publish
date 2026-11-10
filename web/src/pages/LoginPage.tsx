import { useState, type FormEvent } from "react";
import { api, errMsg } from "../api";
import { useAuth } from "../auth";

export default function LoginPage() {
  const { refresh } = useAuth();
  const [mode, setMode] = useState<"login" | "register">("login");
  const [uid, setUid] = useState("");
  const [nickname, setNickname] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setError("");
    setBusy(true);
    try {
      if (mode === "login") await api.login(uid, password);
      else await api.register(uid, nickname, password);
      await refresh();
    } catch (err) {
      setError(errMsg(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <form className="card narrow" onSubmit={submit}>
      <h1>{mode === "login" ? "登录" : "注册"}</h1>
      <label>
        用户 ID
        <input
          value={uid}
          onChange={(e) => setUid(e.target.value)}
          autoComplete="username"
          placeholder={mode === "register" ? "3-20 位英文字母或数字，不可重复" : ""}
          pattern="[A-Za-z0-9]{3,20}"
          required
        />
      </label>
      {mode === "register" && (
        <label>
          用户名
          <input value={nickname} onChange={(e) => setNickname(e.target.value)} maxLength={32} placeholder="昵称，可重复" required />
        </label>
      )}
      <label>
        密码
        <input
          type="password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          autoComplete={mode === "login" ? "current-password" : "new-password"}
          minLength={mode === "register" ? 8 : undefined}
          required
        />
      </label>
      {error && <p className="error">{error}</p>}
      <button type="submit" disabled={busy}>
        {mode === "login" ? "登录" : "注册"}
      </button>
      <button
        type="button"
        className="link"
        onClick={() => {
          setMode(mode === "login" ? "register" : "login");
          setError("");
        }}
      >
        {mode === "login" ? "没有账号？去注册" : "已有账号？去登录"}
      </button>
    </form>
  );
}
