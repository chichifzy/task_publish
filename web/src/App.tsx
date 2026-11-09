import type { ReactNode } from "react";
import { Link, Navigate, Route, Routes } from "react-router";
import { useAuth } from "./auth";
import LoginPage from "./pages/LoginPage";
import TasksPage from "./pages/TasksPage";
import AdminPage from "./pages/AdminPage";

export default function App() {
  const { user, loading, logout } = useAuth();
  if (loading) return <p className="container">加载中…</p>;

  return (
    <>
      {user && (
        <header className="header">
          <nav>
            <Link to="/">任务大厅</Link>
            {user.role === "admin" && <Link to="/admin">任务管理</Link>}
          </nav>
          <span>
            {user.username}
            {user.role === "admin" && "（管理员）"}
            <button className="link" onClick={logout}>
              退出
            </button>
          </span>
        </header>
      )}
      <main className="container">
        <Routes>
          <Route path="/login" element={user ? <Navigate to="/" replace /> : <LoginPage />} />
          <Route path="/" element={<RequireAuth>{<TasksPage />}</RequireAuth>} />
          <Route path="/admin" element={<RequireAuth admin>{<AdminPage />}</RequireAuth>} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </main>
    </>
  );
}

// 前端路由守卫只影响页面展示，真正的权限校验在后端。
function RequireAuth({ admin, children }: { admin?: boolean; children: ReactNode }) {
  const { user } = useAuth();
  if (!user) return <Navigate to="/login" replace />;
  if (admin && user.role !== "admin") return <Navigate to="/" replace />;
  return children;
}
