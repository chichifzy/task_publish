package server

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"
)

// 积分商城：官方商城由管理员维护，用通用积分购买；专有商城属于高级用户，用该高级用户的专有积分购买。

// boundStatuses：普通用户可以看到并购买这些绑定关系中高级用户的专有商城
const boundStatuses = `('bound', 'pending_unbind')`

type Product struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Detail    string `json:"detail"`
	Cost      int64  `json:"cost"`
	CreatedAt string `json:"createdAt"`
}

type superShop struct {
	Super    userRef   `json:"super"`
	Points   int64     `json:"points"` // 普通用户在该商城可用的专有积分
	Products []Product `json:"products"`
}

type shopResp struct {
	Official  []Product   `json:"official"`
	Exclusive []superShop `json:"exclusive"`
}

func (s *Server) listProducts(r *http.Request, ownerID sql.NullInt64) ([]Product, error) {
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT id, name, detail, cost, created_at FROM products
		WHERE deleted = 0 AND owner_id IS ? ORDER BY id DESC`, ownerID)
	return scanList(rows, err, func(p *Product) []any {
		return []any{&p.ID, &p.Name, &p.Detail, &p.Cost, &p.CreatedAt}
	})
}

// getShop 所有人都能看到官方商城；高级用户看到自己的专有商城，普通用户看到已绑定高级用户的专有商城。
func (s *Server) getShop(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	resp := shopResp{Exclusive: []superShop{}}
	var err error
	if resp.Official, err = s.listProducts(r, sql.NullInt64{}); err != nil {
		internalError(w, r, err)
		return
	}
	var shops []superShop
	var ids []int64
	switch u.Role {
	case RoleSuper:
		shops = []superShop{{Super: userRef{u.UID, u.Nickname}}}
		ids = []int64{u.ID}
	case RoleMember:
		rows, qerr := s.db.QueryContext(r.Context(), `
			SELECT s.id, s.uid, s.nickname, COALESCE(e.points, 0)
			FROM bindings b JOIN users s ON s.id = b.super_id
			LEFT JOIN exclusive_points e ON e.user_id = b.member_id AND e.super_id = b.super_id
			WHERE b.member_id = ? AND b.status IN `+boundStatuses+` AND s.role = 'super'
			ORDER BY s.uid`, u.ID)
		type row struct {
			id   int64
			shop superShop
		}
		list, serr := scanList(rows, qerr, func(v *row) []any {
			return []any{&v.id, &v.shop.Super.UID, &v.shop.Super.Nickname, &v.shop.Points}
		})
		if serr != nil {
			internalError(w, r, serr)
			return
		}
		for _, v := range list {
			shops = append(shops, v.shop)
			ids = append(ids, v.id)
		}
	}
	for i := range shops {
		if shops[i].Products, err = s.listProducts(r, sql.NullInt64{Int64: ids[i], Valid: true}); err != nil {
			internalError(w, r, err)
			return
		}
	}
	if shops != nil {
		resp.Exclusive = shops
	}
	writeJSON(w, http.StatusOK, resp)
}

type productInput struct {
	Name   string `json:"name"`
	Detail string `json:"detail"`
	Cost   int64  `json:"cost"`
}

func (in *productInput) validate(official bool) error {
	in.Name = strings.TrimSpace(in.Name)
	in.Detail = strings.TrimSpace(in.Detail)
	if n := utf8.RuneCountInString(in.Name); n < 1 || n > 50 {
		return errStatus(http.StatusBadRequest, "商品名称需为 1-50 个字符")
	}
	if utf8.RuneCountInString(in.Detail) > 1000 {
		return errStatus(http.StatusBadRequest, "商品详情不能超过 1000 个字符")
	}
	// 官方商城的商品可以免费
	if official && in.Cost == 0 {
		return nil
	}
	if in.Cost < 1 || in.Cost > maxPoints {
		if official {
			return errStatus(http.StatusBadRequest, "所需积分需为不小于 0 的整数")
		}
		return errStatus(http.StatusBadRequest, "所需积分需为不小于 1 的整数")
	}
	return nil
}

// shopOwner 管理员维护官方商城（owner_id 为空），高级用户维护自己的专有商城。
func shopOwner(u *User) sql.NullInt64 {
	return sql.NullInt64{Int64: u.ID, Valid: !u.IsAdmin()}
}

func (s *Server) createProduct(w http.ResponseWriter, r *http.Request) {
	var in productInput
	if !decodeJSON(w, r, &in) {
		return
	}
	if err := in.validate(currentUser(r).IsAdmin()); err != nil {
		fail(w, r, err)
		return
	}
	res, err := s.db.ExecContext(r.Context(),
		`INSERT INTO products (owner_id, name, detail, cost) VALUES (?, ?, ?, ?)`,
		shopOwner(currentUser(r)), in.Name, in.Detail, in.Cost)
	if err != nil {
		internalError(w, r, err)
		return
	}
	id, _ := res.LastInsertId()
	writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
}

func (s *Server) updateProduct(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in productInput
	if !decodeJSON(w, r, &in) {
		return
	}
	if err := in.validate(currentUser(r).IsAdmin()); err != nil {
		fail(w, r, err)
		return
	}
	// 已购买的记录保存了快照，修改商品不影响它们
	res, err := s.db.ExecContext(r.Context(), `
		UPDATE products SET name = ?, detail = ?, cost = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
		WHERE id = ? AND owner_id IS ? AND deleted = 0`,
		in.Name, in.Detail, in.Cost, id, shopOwner(currentUser(r)))
	if err != nil {
		internalError(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, "商品不存在")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteProduct(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	res, err := s.db.ExecContext(r.Context(),
		`UPDATE products SET deleted = 1 WHERE id = ? AND owner_id IS ? AND deleted = 0`,
		id, shopOwner(currentUser(r)))
	if err != nil {
		internalError(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, "商品不存在")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// buyProduct 普通用户购买商品：官方商品扣通用积分，专有商品扣对应高级用户的专有积分，余额不足不能购买。
func (s *Server) buyProduct(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	u := currentUser(r)
	err := s.withTx(r.Context(), func(tx *sql.Tx) error {
		var owner sql.NullInt64
		var name, detail string
		var cost int64
		err := tx.QueryRowContext(r.Context(),
			`SELECT owner_id, name, detail, cost FROM products WHERE id = ? AND deleted = 0`, id).
			Scan(&owner, &name, &detail, &cost)
		if errors.Is(err, sql.ErrNoRows) {
			return errStatus(http.StatusNotFound, "商品不存在或已下架")
		}
		if err != nil {
			return err
		}
		var res sql.Result
		if owner.Valid {
			var bound bool
			if err := tx.QueryRowContext(r.Context(), `
				SELECT EXISTS (SELECT 1 FROM bindings b JOIN users s ON s.id = b.super_id
				WHERE b.member_id = ? AND b.super_id = ? AND b.status IN `+boundStatuses+` AND s.role = 'super')`,
				u.ID, owner.Int64).Scan(&bound); err != nil {
				return err
			}
			if !bound {
				return errStatus(http.StatusForbidden, "只能购买已绑定高级用户的专有商品")
			}
			res, err = tx.ExecContext(r.Context(), `
				UPDATE exclusive_points SET points = points - ?
				WHERE user_id = ? AND super_id = ? AND points >= ?`, cost, u.ID, owner.Int64, cost)
		} else {
			res, err = tx.ExecContext(r.Context(),
				`UPDATE users SET points = points - ? WHERE id = ? AND (? = 0 OR points >= ?)`, cost, u.ID, cost, cost)
		}
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			if owner.Valid {
				return errStatus(http.StatusConflict, "专有积分不足")
			}
			return errStatus(http.StatusConflict, "通用积分不足")
		}
		_, err = tx.ExecContext(r.Context(), `
			INSERT INTO purchases (product_id, user_id, super_id, name, detail, cost) VALUES (?, ?, ?, ?, ?, ?)`,
			id, u.ID, owner, name, detail, cost)
		return err
	})
	if err != nil {
		fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

type Purchase struct {
	ID         int64    `json:"id"`
	Name       string   `json:"name"`
	Detail     string   `json:"detail"`
	Cost       int64    `json:"cost"`
	Super      *userRef `json:"super"` // 为空表示官方商城
	Buyer      userRef  `json:"buyer"`
	Reminded   bool     `json:"reminded"`
	RedeemedAt *string  `json:"redeemedAt"`
	CreatedAt  string   `json:"createdAt"`
}

const purchaseSelect = `
	SELECT p.id, p.name, p.detail, p.cost, s.uid, s.nickname, b.uid, b.nickname, p.reminded, p.redeemed_at, p.created_at
	FROM purchases p JOIN users b ON b.id = p.user_id LEFT JOIN users s ON s.id = p.super_id `

func scanPurchases(rows *sql.Rows, err error) ([]Purchase, error) {
	type row struct {
		Purchase
		superUID, superNick sql.NullString
	}
	list, err := scanList(rows, err, func(v *row) []any {
		return []any{&v.ID, &v.Name, &v.Detail, &v.Cost, &v.superUID, &v.superNick,
			&v.Buyer.UID, &v.Buyer.Nickname, &v.Reminded, &v.RedeemedAt, &v.CreatedAt}
	})
	out := make([]Purchase, len(list))
	for i, v := range list {
		out[i] = v.Purchase
		if v.superUID.Valid {
			out[i].Super = &userRef{v.superUID.String, v.superNick.String}
		}
	}
	return out, err
}

// listMyPurchases 普通用户的已购项目，未核销的排在前面。查看后清除催核销提醒。
func (s *Server) listMyPurchases(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	rows, err := s.db.QueryContext(r.Context(), purchaseSelect+`
		WHERE p.user_id = ? AND p.buyer_hidden = 0 ORDER BY p.redeemed_at IS NOT NULL, p.id DESC`, u.ID)
	list, err := scanPurchases(rows, err)
	if err == nil {
		_, err = s.db.ExecContext(r.Context(),
			`UPDATE purchases SET reminded = 0 WHERE user_id = ? AND reminded = 1`, u.ID)
	}
	if err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) redeemPurchase(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	res, err := s.db.ExecContext(r.Context(), `
		UPDATE purchases SET redeemed_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now'), reminded = 0
		WHERE id = ? AND user_id = ? AND redeemed_at IS NULL`, id, currentUser(r).ID)
	if err != nil {
		internalError(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, "记录不存在或已核销")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// listSales 售出记录：管理员看官方商城，高级用户看自己的专有商城。
func (s *Server) listSales(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), purchaseSelect+`
		WHERE p.super_id IS ? AND p.seller_hidden = 0 ORDER BY p.redeemed_at IS NOT NULL, p.id DESC`, shopOwner(currentUser(r)))
	list, err := scanPurchases(rows, err)
	if err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// remindRedeem 高级用户催普通用户核销自己商城中售出的商品。
func (s *Server) remindRedeem(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	res, err := s.db.ExecContext(r.Context(), `
		UPDATE purchases SET reminded = 1 WHERE id = ? AND super_id = ? AND redeemed_at IS NULL`,
		id, currentUser(r).ID)
	if err != nil {
		internalError(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, "记录不存在或已核销")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// deletePurchase 普通用户从“已购项目”中删除已核销的记录，只对自己隐藏。
func (s *Server) deletePurchase(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	res, err := s.db.ExecContext(r.Context(), `
		UPDATE purchases SET buyer_hidden = 1
		WHERE id = ? AND user_id = ? AND redeemed_at IS NOT NULL AND buyer_hidden = 0`, id, currentUser(r).ID)
	if err != nil {
		internalError(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, "记录不存在或尚未核销")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// deleteSale 从“售出记录”中删除，只对卖家隐藏。管理员可以直接删除；
// 高级用户只能删除已核销的记录，未核销的记录还会限制向买家派发任务。
func (s *Server) deleteSale(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	u := currentUser(r)
	res, err := s.db.ExecContext(r.Context(), `
		UPDATE purchases SET seller_hidden = 1
		WHERE id = ? AND super_id IS ? AND seller_hidden = 0 AND (? OR redeemed_at IS NOT NULL)`,
		id, shopOwner(u), u.IsAdmin())
	if err != nil {
		internalError(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, "记录不存在或尚未核销")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
