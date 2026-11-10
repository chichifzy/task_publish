import { useState, type ReactNode } from "react";

export type Tag = { text: string; tone?: "green" | "gray" | "blue" | "orange" | "red" };

type Props = {
  /** 任务编号，如 G-12 / P-35 */
  code: string;
  points: number;
  /** 完成后获得专有积分（高级用户派发） */
  exclusive?: boolean;
  tags?: Tag[];
  /** 任务描述，默认最多显示两行，超出部分用“…”省略，点击展开 */
  description?: string;
  /** 描述下方的补充内容（完成情况、审核意见、表单等） */
  children?: ReactNode;
  actions?: ReactNode;
};

export default function TaskCard({ code, points, exclusive, tags = [], description, children, actions }: Props) {
  const [expanded, setExpanded] = useState(false);
  return (
    <div className="card">
      <div className="card-head">
        <span className="code">{code}</span>
        <span className="points">
          {points} {exclusive ? "专有积分" : "积分"}
        </span>
        {tags.map((t) => (
          <span key={t.text} className={`tag ${t.tone ?? "gray"}`}>
            {t.text}
          </span>
        ))}
      </div>
      {description !== undefined && (
        <p
          className={`desc ${expanded ? "" : "clamp"}`}
          title={expanded ? "点击收起" : "点击查看全文"}
          onClick={() => setExpanded(!expanded)}
        >
          {description}
        </p>
      )}
      {children}
      {actions && <div className="row actions">{actions}</div>}
    </div>
  );
}
