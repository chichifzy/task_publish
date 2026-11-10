import type { Assignment, AssignmentStatus, Role, UserRef } from "./api";
import type { Tag } from "./components/TaskCard";

export const roleLabel: Record<Role, string> = {
  admin: "管理员",
  super: "高级用户",
  member: "普通用户",
};

export const statusTag: Record<AssignmentStatus, Tag> = {
  in_progress: { text: "进行中", tone: "blue" },
  submitted: { text: "待审核", tone: "orange" },
  completed: { text: "已完成", tone: "green" },
  abandoned: { text: "已放弃", tone: "gray" },
};

export const userLabel = (u: UserRef) => `${u.nickname}（${u.uid}）`;

/** 任务来源。withDispatcher 为 true 时显示派发者。 */
export function sourceText(a: Assignment, withDispatcher = false) {
  switch (a.source) {
    case "claim":
      return "主动接取";
    case "draw":
      return "随机抽取";
    case "dispatch":
      return withDispatcher ? `${userLabel(a.reviewer)} 派发` : "派发";
  }
}
