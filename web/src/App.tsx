import { useEffect, useRef, useState, type ReactNode } from "react";
import { Navigate, NavLink, Route, Routes, useNavigate } from "react-router";
import { api, errMsg, type Me, type Role } from "./api";
import { useAuth } from "./auth";
import { roleLabel } from "./labels";
import LoginPage from "./pages/LoginPage";
import PublicPage from "./pages/PublicPage";
import PrivatePage from "./pages/PrivatePage";
import MyTasksPage from "./pages/MyTasksPage";
import ReviewsPage from "./pages/ReviewsPage";
import SettingsPage from "./pages/SettingsPage";
import UsersPage from "./pages/UsersPage";
import ProfilePage from "./pages/ProfilePage";

const pages: Record<string, ReactNode> = {
  "/": <PublicPage />,
  "/private": <PrivatePage />,
  "/mine": <MyTasksPage />,
  "/reviews": <ReviewsPage />,
  "/settings": <SettingsPage />,
  "/users": <UsersPage />,
};

type NavItem = { to: string; label: string; badge?: number };

// 每个角色能看到的页面。前端只控制展示，真正的权限校验在后端。
function navFor(me: Me): NavItem[] {
  const reviews: NavItem = { to: "/reviews", label: "审核", badge: me.pendingReviews };
  switch (me.role) {
    case "admin":
      return [{ to: "/", label: "公共仓库" }, { to: "/private", label: "私有仓库" }, reviews, { to: "/users", label: "用户管理" }];
    case "super":
      return [{ to: "/", label: "公共仓库" }, { to: "/private", label: "私有仓库" }, reviews];
    case "member": {
      const items: NavItem[] = [
        { to: "/", label: "任务大厅" },
        { to: "/mine", label: "我的任务" },
        { to: "/settings", label: "设置" },
      ];
      // 被撤销的高级用户：可查看冻结的私有仓库，审核之前派发的任务
      if (me.hasPrivateTasks) items.push({ to: "/private", label: "私有仓库（已冻结）" });
      if (me.pendingReviews > 0) items.push(reviews);
      return items;
    }
  }
}

/** 侧边栏底部的头像和弹出菜单。点击外部或按 Esc 关闭。 */
function UserMenu({ me, onLogout }: { me: Me; onLogout: () => void }) {
  const [open, setOpen] = useState(false);
  const [switching, setSwitching] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  const navigate = useNavigate();
  const { refresh } = useAuth();

  const switchTo = async (role: Role) => {
    setOpen(false);
    setSwitching(false);
    try {
      await api.switchIdentity(role);
      navigate("/");
      await refresh();
    } catch (e) {
      alert(errMsg(e));
    }
  };

  useEffect(() => {
    if (!open) return;
    const onClick = (e: MouseEvent) => {
      if (!ref.current?.contains(e.target as Node)) setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false);
    };
    document.addEventListener("mousedown", onClick);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onClick);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  return (
    <div className="user-menu" ref={ref}>
      {open && (
        <div className="popover" role="menu">
          <button
            role="menuitem"
            onClick={() => {
              setOpen(false);
              navigate("/profile");
            }}
          >
            个人设置
          </button>
          {me.canSwitchIdentity && (
            <button role="menuitem" aria-expanded={switching} onClick={() => setSwitching(!switching)}>
              切换身份 {switching ? "▾" : "▸"}
            </button>
          )}
          {me.canSwitchIdentity &&
            switching &&
            (["admin", "super", "member"] as const).map((role) => (
              <button
                key={role}
                role="menuitem"
                className="sub-item"
                disabled={role === me.role}
                onClick={() => switchTo(role)}
              >
                {roleLabel[role]}
                {role === me.role && "（当前）"}
              </button>
            ))}
          <button role="menuitem" className="danger-text" onClick={onLogout}>
            退出登录
          </button>
        </div>
      )}
      <button className="user-chip" aria-haspopup="menu" aria-expanded={open} onClick={() => setOpen(!open)}>
        <span className="avatar">{Array.from(me.nickname)[0] ?? "?"}</span>
        <span className="user-text">
          <span className="user-name">{me.nickname}</span>
          <span className="user-role">
            {roleLabel[me.role]} · {me.uid}
          </span>
        </span>
      </button>
    </div>
  );
}

export default function App() {
  const { user, loading, logout } = useAuth();
  if (loading) return <p className="loading">加载中…</p>;

  if (!user) {
    return (
      <main className="auth-page">
        <Routes>
          <Route path="/login" element={<LoginPage />} />
          <Route path="*" element={<Navigate to="/login" replace />} />
        </Routes>
      </main>
    );
  }

  const nav = navFor(user);
  return (
    <div className="layout">
      <aside className="sidebar">
        <div className="brand">任务发布系统</div>
        {user.role === "member" && (
          <div className="points-box">
            <span className="muted">我的积分</span>
            <strong>{user.points}</strong>
          </div>
        )}
        <nav className="side-nav">
          {nav.map((item) => (
            <NavLink key={item.to} to={item.to} end>
              <span>{item.label}</span>
              {!!item.badge && <span className="badge">{item.badge}</span>}
            </NavLink>
          ))}
        </nav>
        <UserMenu me={user} onLogout={logout} />
      </aside>
      {/* key 随身份变化，切换身份后页面重新挂载并重新加载数据 */}
      <main className="content" key={user.uid}>
        <div className="container">
          <Routes>
            {nav.map((item) => (
              <Route key={item.to} path={item.to} element={pages[item.to]} />
            ))}
            <Route path="/profile" element={<ProfilePage />} />
            <Route path="*" element={<Navigate to="/" replace />} />
          </Routes>
        </div>
      </main>
    </div>
  );
}
