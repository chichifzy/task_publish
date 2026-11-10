import type { ReactNode } from "react";
import { Navigate, NavLink, Route, Routes } from "react-router";
import type { Me } from "./api";
import { useAuth } from "./auth";
import { roleLabel } from "./labels";
import LoginPage from "./pages/LoginPage";
import PublicPage from "./pages/PublicPage";
import PrivatePage from "./pages/PrivatePage";
import MyTasksPage from "./pages/MyTasksPage";
import ReviewsPage from "./pages/ReviewsPage";
import SettingsPage from "./pages/SettingsPage";
import UsersPage from "./pages/UsersPage";

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

export default function App() {
  const { user, loading, logout } = useAuth();
  if (loading) return <p className="container">加载中…</p>;

  if (!user) {
    return (
      <main className="container">
        <Routes>
          <Route path="/login" element={<LoginPage />} />
          <Route path="*" element={<Navigate to="/login" replace />} />
        </Routes>
      </main>
    );
  }

  const nav = navFor(user);
  return (
    <>
      <header className="header">
        <nav>
          {nav.map((item) => (
            <NavLink key={item.to} to={item.to} end>
              {item.label}
              {!!item.badge && <span className="badge">{item.badge}</span>}
            </NavLink>
          ))}
        </nav>
        <span className="row">
          <span>
            {user.nickname}（{user.uid}）· {roleLabel[user.role]}
            {user.role === "member" && ` · ${user.points} 积分`}
          </span>
          <button className="link" onClick={logout}>
            退出
          </button>
        </span>
      </header>
      <main className="container">
        <Routes>
          {nav.map((item) => (
            <Route key={item.to} path={item.to} element={pages[item.to]} />
          ))}
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </main>
    </>
  );
}
