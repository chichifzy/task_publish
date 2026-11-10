// 对后端 JSON API 的薄封装。Cookie 由浏览器自动携带。

export type Role = "admin" | "super" | "member";

export type Me = {
  uid: string;
  nickname: string;
  role: Role;
  points: number;
  acceptAdminDispatch: boolean;
  pendingReviews: number;
  hasPrivateTasks: boolean;
};

export type Task = {
  id: number;
  public: boolean;
  description: string;
  points: number;
  visibility: "all" | "privileged" | "";
  enabled: boolean;
  copiedFrom?: number;
  createdAt: string;
  updatedAt: string;
  activeCount?: number;
  myActive: boolean;
};

export type AssignmentStatus = "in_progress" | "submitted" | "completed" | "abandoned";

export type UserRef = { uid: string; nickname: string };

export type Assignment = {
  id: number;
  taskId: number;
  taskPublic: boolean;
  source: "claim" | "draw" | "dispatch";
  description: string;
  points: number;
  status: AssignmentStatus;
  submission: string;
  reviewNote: string;
  createdAt: string;
  updatedAt: string;
  user: UserRef;
  reviewer: UserRef;
};

export type PublicTaskInput = {
  description: string;
  points: number;
  visibility: "all" | "privileged";
  enabled: boolean;
};

export type PrivateTaskInput = { description: string; points: number };

export type DispatchResult = { uid: string; ok: boolean; error?: string };

export type SuperInfo = UserRef & { authorized: boolean };
export type AuthorizationInfo = UserRef & { active: boolean };
export type UserInfo = UserRef & { role: Role; points: number; createdAt: string };

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

const q = encodeURIComponent;

export const api = {
  me: () => request<Me>("GET", "/me"),
  login: (uid: string, password: string) => request<void>("POST", "/auth/login", { uid, password }),
  register: (uid: string, nickname: string, password: string) =>
    request<void>("POST", "/auth/register", { uid, nickname, password }),
  logout: () => request<void>("POST", "/auth/logout"),
  updateSettings: (acceptAdminDispatch: boolean) =>
    request<void>("PUT", "/me/settings", { acceptAdminDispatch }),

  listPublic: () => request<Task[]>("GET", "/public/tasks"),
  createPublic: (input: PublicTaskInput) => request<{ id: number }>("POST", "/public/tasks", input),
  updatePublic: (id: number, input: PublicTaskInput) => request<void>("PUT", `/public/tasks/${id}`, input),
  deletePublic: (id: number) => request<void>("DELETE", `/public/tasks/${id}`),
  claimPublic: (id: number) => request<{ assignmentId: number }>("POST", `/public/tasks/${id}/claim`),
  copyPublic: (id: number) => request<{ id: number }>("POST", `/public/tasks/${id}/copy`),

  currentDraw: () => request<{ points: number } | null>("GET", "/draw"),
  draw: () => request<{ points: number }>("POST", "/draw"),
  acceptDraw: () => request<{ assignmentId: number }>("POST", "/draw/accept"),
  discardDraw: () => request<void>("DELETE", "/draw"),

  listPrivate: () => request<{ frozen: boolean; tasks: Task[] }>("GET", "/private/tasks"),
  createPrivate: (input: PrivateTaskInput) => request<{ id: number }>("POST", "/private/tasks", input),
  updatePrivate: (id: number, input: PrivateTaskInput) => request<void>("PUT", `/private/tasks/${id}`, input),
  deletePrivate: (id: number) => request<void>("DELETE", `/private/tasks/${id}`),
  dispatch: (id: number, uids: string[]) =>
    request<DispatchResult[]>("POST", `/private/tasks/${id}/dispatch`, { uids }),
  dispatchTargets: () => request<UserRef[]>("GET", "/dispatch-targets"),

  myAssignments: () => request<Assignment[]>("GET", "/assignments/mine"),
  submit: (id: number, submission: string) => request<void>("POST", `/assignments/${id}/submit`, { submission }),
  abandon: (id: number) => request<{ penalty: number; points: number }>("POST", `/assignments/${id}/abandon`),
  reviews: () => request<Assignment[]>("GET", "/reviews"),
  review: (id: number, approve: boolean, note: string) =>
    request<void>("POST", `/assignments/${id}/review`, { approve, note }),

  searchSupers: (query: string) => request<SuperInfo[]>("GET", `/supers?q=${q(query)}`),
  authorizations: () => request<AuthorizationInfo[]>("GET", "/authorizations"),
  authorize: (uid: string) => request<void>("POST", "/authorizations", { uid }),
  unauthorize: (uid: string) => request<void>("DELETE", `/authorizations/${q(uid)}`),

  listUsers: (query: string) => request<UserInfo[]>("GET", `/admin/users?q=${q(query)}`),
  setRole: (uid: string, role: "super" | "member") => request<void>("PUT", `/admin/users/${q(uid)}/role`, { role }),
};

export const taskCode = (id: number, isPublic: boolean) => `${isPublic ? "G" : "P"}-${id}`;

export const errMsg = (e: unknown) => (e instanceof Error ? e.message : String(e));
