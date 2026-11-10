import { useRef, useState, type FormEvent, type ReactNode } from "react";
import { api, type Product, type ProductInput, type Purchase } from "../api";
import { useMe } from "../auth";
import { useAction, useLoader } from "../hooks";
import { userLabel } from "../labels";
import Messages from "../components/Messages";
import Modal, { type ModalHandle } from "../components/Modal";
import { useConfirm } from "../components/Confirm";

/**
 * 积分商城。管理员维护官方商城，高级用户维护自己的专有商城；
 * 普通用户用通用积分购买官方商品，用专有积分购买对应高级用户的商品。
 */
export default function ShopPage() {
  const { me, refresh } = useMe();
  const { data, error: loadError, load } = useLoader(api.shop);
  const { run, error, notice, setNotice } = useAction(() => {
    load();
    refresh();
  });
  const confirm = useConfirm();
  // null：关闭；{ product: null }：新建；{ product }：编辑
  const [form, setForm] = useState<{ product: Product | null } | null>(null);
  const [panel, setPanel] = useState<"purchases" | "sales" | null>(null);
  const isMember = me.role === "member";
  const isAdmin = me.role === "admin";

  const buy = async (p: Product, wallet: string) => {
    if (await confirm({ title: "购买商品", message: p.cost === 0 ? `免费领取「${p.name}」？` : `花费 ${p.cost} ${wallet}购买「${p.name}」？`, confirmText: "购买" }))
      run(() => api.buyProduct(p.id), `已购买「${p.name}」，可在“已购项目”中核销`);
  };

  const remove = async (p: Product) => {
    if (await confirm({ title: "删除商品", message: `删除「${p.name}」？已售出的记录不受影响。`, confirmText: "删除", danger: true }))
      run(() => api.deleteProduct(p.id), `已删除「${p.name}」`);
  };

  const list = (products: Product[], editable: boolean, wallet?: { name: string; balance: number }) =>
    products.length === 0 ? (
      <p className="muted">暂无商品</p>
    ) : (
      products.map((p) => (
        <div className="card shop-item" key={p.id}>
          <div className="shop-item-body">
            <div className="card-head">
              <b>{p.name}</b>
              <span className="tag orange">{p.cost === 0 ? "免费" : `${p.cost} 积分`}</span>
            </div>
            {p.detail && <p className="note">{p.detail}</p>}
          </div>
          {(editable || wallet) && (
            <div className="row shop-item-actions">
              {wallet && (
                <button disabled={p.cost > 0 && wallet.balance < p.cost} onClick={() => buy(p, wallet.name)}>
                  {p.cost > 0 && wallet.balance < p.cost ? "积分不足" : "购买"}
                </button>
              )}
              {editable && (
                <>
                  <button className="secondary" onClick={() => setForm({ product: p })}>
                    编辑
                  </button>
                  <button className="danger" onClick={() => remove(p)}>
                    删除
                  </button>
                </>
              )}
            </div>
          )}
        </div>
      ))
    );

  return (
    <>
      {form && (
        <ProductForm
          product={form.product}
          official={isAdmin}
          onSaved={(msg) => {
            setForm(null);
            setNotice(msg);
            load();
          }}
          onCancel={() => setForm(null)}
        />
      )}
      {panel === "purchases" && (
        <PurchasesModal
          onClose={() => {
            setPanel(null);
            refresh();
          }}
        />
      )}
      {panel === "sales" && <SalesModal canRemind={!isAdmin} onClose={() => setPanel(null)} />}

      <div className="page-head">
        <h2>积分商城</h2>
        <div className="row">
          {isMember ? (
            <button onClick={() => setPanel("purchases")}>
              已购项目{me.shopAlert && <span className="dot" aria-label="有催核销提醒" />}
            </button>
          ) : (
            <>
              <button onClick={() => setForm({ product: null })}>新建商品</button>
              <button className="secondary" onClick={() => setPanel("sales")}>
                售出记录
              </button>
            </>
          )}
        </div>
      </div>
      <Messages error={error || loadError} notice={notice} />

      {data && (
        <>
          <h3>官方商城</h3>
          <p className="muted">
            {isMember ? `使用通用积分购买，当前通用积分 ${me.points}` : "普通用户使用通用积分购买"}
          </p>
          {list(data.official, isAdmin, isMember ? { name: "通用积分", balance: me.points } : undefined)}

          {!isAdmin && (
            <>
              <h3>专有商城</h3>
              {data.exclusive.length === 0 && <p className="muted">绑定高级用户后可以看到对方的专有商城</p>}
              {data.exclusive.map((shop) => (
                <section key={shop.super.uid}>
                  {isMember ? (
                    <p className="muted">
                      <b>{userLabel(shop.super)}</b> 的商城 · 使用其专有积分购买，当前可用 {shop.points}
                    </p>
                  ) : (
                    <p className="muted">普通用户使用你的专有积分购买</p>
                  )}
                  {list(shop.products, !isMember, isMember ? { name: `${shop.super.nickname} 的专有积分`, balance: shop.points } : undefined)}
                </section>
              ))}
            </>
          )}
        </>
      )}
    </>
  );
}

function ProductForm({
  product,
  official,
  onSaved,
  onCancel,
}: {
  product: Product | null;
  official: boolean;
  onSaved: (notice: string) => void;
  onCancel: () => void;
}) {
  const [form, setForm] = useState<ProductInput>(
    product ? { name: product.name, detail: product.detail, cost: product.cost } : { name: "", detail: "", cost: 1 },
  );
  const { run, error, busy } = useAction(() => {});
  const modalRef = useRef<ModalHandle>(null);
  const close = (then: () => void) => (modalRef.current ? modalRef.current.close(then) : then());

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const ok = await run(() => (product ? api.updateProduct(product.id, form) : api.createProduct(form)));
    if (ok) close(() => onSaved(product ? `已保存「${form.name.trim()}」` : "已创建"));
  };

  return (
    <Modal title={product ? "编辑商品" : "新建商品"} onClose={onCancel} ref={modalRef}>
      <form className="modal-body" onSubmit={submit}>
        <label>
          商品名称
          <input maxLength={50} value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} required />
        </label>
        <label>
          商品详情（可选）
          <textarea rows={3} maxLength={1000} value={form.detail} onChange={(e) => setForm({ ...form, detail: e.target.value })} />
        </label>
        <label>
          所需积分
          <input
            type="number"
            min={official ? 0 : 1}
            step={1}
            value={Number.isNaN(form.cost) ? "" : form.cost}
            onChange={(e) => setForm({ ...form, cost: e.target.valueAsNumber })}
            required
          />
        </label>
        <Messages error={error} />
        <div className="modal-foot">
          <button type="button" className="secondary" onClick={() => close(onCancel)}>
            取消
          </button>
          <button type="submit" disabled={busy}>
            {product ? "保存" : "创建"}
          </button>
        </div>
      </form>
    </Modal>
  );
}

const fmt = (t: string) => t.slice(0, 16).replace("T", " ");

function PurchaseItem({ p, who, actions }: { p: Purchase; who: string; actions?: ReactNode }) {
  return (
    <div className="card shop-item">
      <div className="shop-item-body">
        <div className="card-head">
          <b>{p.name}</b>
          <span className="tag orange">{p.cost === 0 ? "免费" : `${p.cost} 积分`}</span>
          <span className="tag blue">{who}</span>
          {p.redeemedAt ? <span className="tag gray">已核销</span> : <span className="tag green">未核销</span>}
          {p.reminded && !p.redeemedAt && <span className="tag red">已催促</span>}
        </div>
        {p.detail && <p className="note">{p.detail}</p>}
        <span className="muted">
          购买于 {fmt(p.createdAt)}
          {p.redeemedAt && ` · 核销于 ${fmt(p.redeemedAt)}`}
        </span>
      </div>
      {actions && <div className="row shop-item-actions">{actions}</div>}
    </div>
  );
}

function PurchasesModal({ onClose }: { onClose: () => void }) {
  const { data, error: loadError, load } = useLoader(api.myPurchases);
  // 打开时会清除催促标记，保留首次加载时的标记用于展示
  const [reminded] = useState(() => new Set<number>());
  data?.forEach((p) => p.reminded && reminded.add(p.id));
  const { run, error, notice } = useAction(load);
  const confirm = useConfirm();

  const redeem = async (p: Purchase) => {
    if (await confirm({ title: "核销", message: `确认核销「${p.name}」？核销后不能撤销。`, confirmText: "核销" }))
      run(() => api.redeem(p.id), `已核销「${p.name}」`);
  };

  const remove = async (p: Purchase) => {
    if (await confirm({ title: "删除记录", message: `从已购项目中删除「${p.name}」？`, confirmText: "删除", danger: true }))
      run(() => api.deletePurchase(p.id), `已删除「${p.name}」`);
  };

  return (
    <Modal title="已购项目" onClose={onClose}>
      <div className="modal-body">
        <Messages error={error || loadError} notice={notice} />
        {data?.length === 0 && <p className="muted">还没有购买过商品</p>}
        {data?.map((p) => (
          <PurchaseItem
            key={p.id}
            p={{ ...p, reminded: reminded.has(p.id) }}
            who={p.super ? `${userLabel(p.super)} 的商城` : "官方商城"}
            actions={
              p.redeemedAt ? (
                <button className="danger" onClick={() => remove(p)}>
                  删除
                </button>
              ) : (
                <button onClick={() => redeem(p)}>核销</button>
              )
            }
          />
        ))}
      </div>
    </Modal>
  );
}

/** canRemind：高级用户可以催核销，只能删除已核销的记录；管理员可以直接删除任意记录。 */
function SalesModal({ canRemind, onClose }: { canRemind: boolean; onClose: () => void }) {
  const { data, error: loadError, load } = useLoader(api.sales);
  const { run, error, notice } = useAction(load);
  const confirm = useConfirm();

  const remove = async (p: Purchase) => {
    if (await confirm({ title: "删除记录", message: `从售出记录中删除「${p.name}」？买家的已购项目不受影响。`, confirmText: "删除", danger: true }))
      run(() => api.deleteSale(p.id), `已删除「${p.name}」的售出记录`);
  };

  return (
    <Modal title="售出记录" onClose={onClose}>
      <div className="modal-body">
        <Messages error={error || loadError} notice={notice} />
        {data?.length === 0 && <p className="muted">还没有售出记录</p>}
        {data?.map((p) => (
          <PurchaseItem
            key={p.id}
            p={p}
            who={`买家 ${userLabel(p.buyer)}`}
            actions={
              <>
                {canRemind && !p.redeemedAt && (
                  <button className="secondary" onClick={() => run(() => api.remind(p.id), `已提醒 ${userLabel(p.buyer)} 核销`)}>
                    {p.reminded ? "再次催核销" : "催核销"}
                  </button>
                )}
                {(!canRemind || p.redeemedAt) && (
                  <button className="danger" onClick={() => remove(p)}>
                    删除
                  </button>
                )}
              </>
            }
          />
        ))}
      </div>
    </Modal>
  );
}
