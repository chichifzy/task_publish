// 对后端 JSON API 的薄封装。Cookie 由浏览器自动携带。

export type User = { id: number; username: string; role: "user" | "admin" };

export type Task = {
  id: number;
  title: string;
  description: string;
  status: "open" | "closed";
  createdAt: string;
  updatedAt: string;
  claimed: boolean;
  claimedByMe: boolean;
  claimedBy?: string;
};

export type TaskInput = Pick<Task, "title" | "description" | "status">;

export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message);
  }
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(`/api${path}`, {
    method,
    headers: body === undefined ? undefined : { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (!res.ok) {
    const data = await res.json().catch(() => null);
    throw new ApiError(res.status, data?.error ?? `请求失败（${res.status}）`);
  }
  if (res.status === 204) return undefined as T;
  return res.json();
}

export const api = {
  me: () => request<User>("GET", "/me"),
  login: (username: string, password: string) =>
    request<User>("POST", "/auth/login", { username, password }),
  register: (username: string, password: string) =>
    request<User>("POST", "/auth/register", { username, password }),
  logout: () => request<void>("POST", "/auth/logout"),

  listTasks: () => request<Task[]>("GET", "/tasks"),
  claimTask: (id: number) => request<void>("POST", `/tasks/${id}/claim`),

  createTask: (input: TaskInput) => request<{ id: number }>("POST", "/admin/tasks", input),
  updateTask: (id: number, input: TaskInput) => request<void>("PUT", `/admin/tasks/${id}`, input),
  deleteTask: (id: number) => request<void>("DELETE", `/admin/tasks/${id}`),
};
