import { useState, type FormEvent } from "react";
import { api } from "../api";
import { useMe } from "../auth";
import { useAction } from "../hooks";
import { roleLabel } from "../labels";
import Messages from "../components/Messages";

/** 所有角色：修改用户名和密码。 */
export default function ProfilePage() {
  const { me, refresh } = useMe();

  const [nickname, setNickname] = useState(me.nickname);
  const profile = useAction(refresh);
  const saveNickname = (e: FormEvent) => {
    e.preventDefault();
    profile.run(() => api.updateProfile(nickname), "用户名已更新");
  };

  const [oldPassword, setOldPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [mismatch, setMismatch] = useState("");
  const password = useAction(() => {
    setOldPassword("");
    setNewPassword("");
    setConfirmPassword("");
  });
  const savePassword = (e: FormEvent) => {
    e.preventDefault();
    if (newPassword !== confirmPassword) {
      setMismatch("两次输入的新密码不一致");
      return;
    }
    setMismatch("");
    password.run(() => api.changePassword(oldPassword, newPassword), "密码已修改，其他设备上的登录已失效");
  };

  return (
    <>
      <h2>个人设置</h2>
      <p className="muted">
        用户 ID：{me.uid} · {roleLabel[me.role]}
      </p>

      <form className="card" onSubmit={saveNickname}>
        <h3>修改用户名</h3>
        <label>
          用户名
          <input value={nickname} onChange={(e) => setNickname(e.target.value)} maxLength={32} required />
        </label>
        <Messages error={profile.error} notice={profile.notice} />
        <div className="row actions">
          <button type="submit" disabled={profile.busy || !nickname.trim() || nickname.trim() === me.nickname}>
            保存
          </button>
        </div>
      </form>

      <form className="card" onSubmit={savePassword}>
        <h3>修改密码</h3>
        <label>
          原密码
          <input
            type="password"
            value={oldPassword}
            onChange={(e) => setOldPassword(e.target.value)}
            autoComplete="current-password"
            required
          />
        </label>
        <label>
          新密码
          <input
            type="password"
            value={newPassword}
            onChange={(e) => setNewPassword(e.target.value)}
            autoComplete="new-password"
            placeholder="8-72 个字符"
            minLength={8}
            maxLength={72}
            required
          />
        </label>
        <label>
          确认新密码
          <input
            type="password"
            value={confirmPassword}
            onChange={(e) => setConfirmPassword(e.target.value)}
            autoComplete="new-password"
            required
          />
        </label>
        <Messages error={mismatch || password.error} notice={password.notice} />
        <div className="row actions">
          <button type="submit" disabled={password.busy}>
            修改密码
          </button>
        </div>
      </form>
    </>
  );
}
