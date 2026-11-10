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
  /** 管理员及其身份账号可以切换身份 */
  canSwitchIdentity: boolean;
  /** 高级用户：有未查看的绑定/解绑申请 */
  newBindRequests: boolean;
  newTasks: boolean;
  /** 普通用户：有被催核销的已购商品 */
  shopAlert: boolean;
  /** 普通用户：在各高级用户处的专有积分（points 为通用积分） */
  exclusivePoints: { super: UserRef; points: number }[];
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

export type AssignmentStatus = "in_progress" | "submitted" | "completed" | "abandoned" | "cancelled";

export type UserRef = { uid: string; nickname: string };

export type Assignment = {
  id: number;
  taskId: number;
  taskPublic: boolean;
  source: "claim" | "draw" | "dispatch";
  description: string;
  points: number;
  /** 高级用户派发的任务：完成后获得派发者（reviewer）的专有积分 */
  exclusive: boolean;
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

export type BindStatus = "pending_bind" | "bound" | "pending_unbind";
export type SuperInfo = UserRef & { status: BindStatus | "blocked" | "" };
export type BindingInfo = UserRef & { status: BindStatus; active: boolean };
export type MemberInfo = UserRef & {
  points: number;
  /** 在当前高级用户处的专有积分 */
  exclusivePoints: number;
  createdAt: string;
  status: BindStatus;
  appliedAt: string;
  boundAt: string | null;
};
export type BlockInfo = UserRef & { role: Role; byMe: boolean; pending: boolean };
export type AdminBlockInfo = {
  a: UserRef;
  b: UserRef;
  blockedBy: string;
  createdAt: string;
  requestId: number | null;
  requester: string | null;
};
export type UserInfo = UserRef & { role: Role; points: number; createdAt: string };

export type Product = { id: number; name: string; detail: string; cost: number; createdAt: string };
export type ProductInput = { name: string; detail: string; cost: number };
export type Shop = {
  official: Product[];
  /** 高级用户：只有自己的商城；普通用户：已绑定高级用户的商城，points 为在该商城可用的专有积分 */
  exclusive: { super: UserRef; points: number; products: Product[] }[];
};
export type Purchase = {
  id: number;
  name: string;
  detail: string;
  cost: number;
  /** 为空表示官方商城 */
  super: UserRef | null;
  buyer: UserRef;
  reminded: boolean;
  redeemedAt: string | null;
  createdAt: string;
};

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
  // 204 或空响应体（如 201 无内容）不解析 JSON
  const text = await res.text();
  return (text ? JSON.parse(text) : undefined) as T;
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
  updateProfile: (nickname: string) => request<void>("PUT", "/me/profile", { nickname }),
  changePassword: (oldPassword: string, newPassword: string) =>
    request<void>("PUT", "/me/password", { oldPassword, newPassword }),
  switchIdentity: (role: Role) => request<void>("POST", "/me/identity", { role }),

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
  dispatchTargets: () => request<(UserRef & { unredeemed: boolean })[]>("GET", "/dispatch-targets"),

  myAssignments: () => request<Assignment[]>("GET", "/assignments/mine"),
  submit: (id: number, submission: string) => request<void>("POST", `/assignments/${id}/submit`, { submission }),
  abandon: (id: number) => request<{ penalty: number }>("POST", `/assignments/${id}/abandon`),
  reviews: () => request<Assignment[]>("GET", "/reviews"),
  review: (id: number, approve: boolean, note: string) =>
    request<void>("POST", `/assignments/${id}/review`, { approve, note }),
  cancelAssignment: (id: number) => request<void>("POST", `/assignments/${id}/cancel`),
  records: () => request<Assignment[]>("GET", "/records"),
  deleteRecord: (id: number) => request<void>("DELETE", `/records/${id}`),

  shop: () => request<Shop>("GET", "/shop"),
  createProduct: (input: ProductInput) => request<{ id: number }>("POST", "/shop/products", input),
  updateProduct: (id: number, input: ProductInput) => request<void>("PUT", `/shop/products/${id}`, input),
  deleteProduct: (id: number) => request<void>("DELETE", `/shop/products/${id}`),
  buyProduct: (id: number) => request<void>("POST", `/shop/products/${id}/buy`),
  myPurchases: () => request<Purchase[]>("GET", "/shop/purchases"),
  redeem: (id: number) => request<void>("POST", `/shop/purchases/${id}/redeem`),
  deletePurchase: (id: number) => request<void>("DELETE", `/shop/purchases/${id}`),
  sales: () => request<Purchase[]>("GET", "/shop/sales"),
  deleteSale: (id: number) => request<void>("DELETE", `/shop/sales/${id}`),
  remind: (id: number) => request<void>("POST", `/shop/sales/${id}/remind`),

  searchSupers: (query: string) => request<SuperInfo[]>("GET", `/supers?q=${q(query)}`),
  myBindings: () => request<BindingInfo[]>("GET", "/bindings"),
  applyBind: (uid: string) => request<void>("POST", "/bindings", { uid }),
  bindingAction: (uid: string, action: "cancel" | "unbind" | "force") =>
    request<void>("POST", `/bindings/${q(uid)}/${action}`),
  members: () => request<MemberInfo[]>("GET", "/members"),
  memberAction: (uid: string, action: "approve" | "reject" | "unbind") =>
    request<void>("POST", `/members/${q(uid)}/${action}`),
  myBlocks: () => request<BlockInfo[]>("GET", "/blocks"),
  requestUnblock: (uid: string) => request<void>("POST", `/blocks/${q(uid)}/request`),

  listUsers: (query: string) => request<UserInfo[]>("GET", `/admin/users?q=${q(query)}`),
  setRole: (uid: string, role: "super" | "member") => request<void>("PUT", `/admin/users/${q(uid)}/role`, { role }),
  adminBlocks: () => request<AdminBlockInfo[]>("GET", "/admin/blocks"),
  resolveBlock: (a: string, b: string, remove: boolean) =>
    request<void>("POST", "/admin/blocks/resolve", { a, b, remove }),
};

export const taskCode = (id: number, isPublic: boolean) => `${isPublic ? "G" : "P"}-${id}`;

export const errMsg = (e: unknown) => (e instanceof Error ? e.message : String(e));
