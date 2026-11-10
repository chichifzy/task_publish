package db

// migrations 按顺序执行，每条只执行一次。
// 修改表结构时在末尾追加新的一条，不要改动已发布的旧条目。
var migrations = []string{
	// #1 初始表结构
	`
CREATE TABLE users (
	id                    INTEGER PRIMARY KEY,
	uid                   TEXT NOT NULL UNIQUE COLLATE NOCASE, -- 用户 ID：字母和数字，不区分大小写
	nickname              TEXT NOT NULL,                        -- 用户名（昵称），可重复
	password_hash         TEXT NOT NULL,
	role                  TEXT NOT NULL DEFAULT 'member' CHECK (role IN ('admin', 'super', 'member')),
	accept_admin_dispatch INTEGER NOT NULL DEFAULT 0 CHECK (accept_admin_dispatch IN (0, 1)),
	points                INTEGER NOT NULL DEFAULT 20,          -- 可以为负数
	created_at            TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
-- 系统中最多只有一个管理员
CREATE UNIQUE INDEX users_single_admin ON users(role) WHERE role = 'admin';

-- 只保存 token 的 SHA-256，数据库泄露也无法直接冒用会话
CREATE TABLE sessions (
	token_hash TEXT PRIMARY KEY,
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	expires_at INTEGER NOT NULL -- Unix 秒
);
CREATE INDEX sessions_user_id ON sessions(user_id);

-- 公共仓库和私有仓库共用一张表：owner_id 为空是公共任务，否则属于该用户的私有仓库。
-- AUTOINCREMENT 保证任务编号不会复用，接取记录里保存的编号始终指向唯一的任务。
CREATE TABLE tasks (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	owner_id    INTEGER REFERENCES users(id),
	description TEXT NOT NULL,
	points      INTEGER NOT NULL CHECK (points >= 1),
	visibility  TEXT NOT NULL DEFAULT 'all' CHECK (visibility IN ('all', 'privileged')), -- 仅公共任务使用
	enabled     INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),                     -- 仅公共任务使用
	copied_from INTEGER REFERENCES tasks(id) ON DELETE SET NULL,
	created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
	updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX tasks_owner_id ON tasks(owner_id);

-- 普通用户授权哪些高级用户可以向自己派发任务
CREATE TABLE authorizations (
	member_id  INTEGER NOT NULL REFERENCES users(id),
	super_id   INTEGER NOT NULL REFERENCES users(id),
	created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
	PRIMARY KEY (member_id, super_id)
) WITHOUT ROWID;
CREATE INDEX authorizations_super_id ON authorizations(super_id);

-- 当前抽到、尚未决定是否接取的任务。记录在服务端，客户端只能拿到积分值。
CREATE TABLE draws (
	user_id    INTEGER PRIMARY KEY REFERENCES users(id),
	task_id    INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
	created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

-- 接取记录。description / points 是接取时的快照，原任务修改或删除都不影响。
-- task_id 不设外键：原任务删除后仍保留编号用于展示。
CREATE TABLE assignments (
	id          INTEGER PRIMARY KEY,
	task_id     INTEGER NOT NULL,
	task_public INTEGER NOT NULL CHECK (task_public IN (0, 1)),
	user_id     INTEGER NOT NULL REFERENCES users(id),
	reviewer_id INTEGER NOT NULL REFERENCES users(id), -- 派发的任务由派发者审核，公共任务由管理员审核
	source      TEXT NOT NULL CHECK (source IN ('claim', 'draw', 'dispatch')),
	description TEXT NOT NULL,
	points      INTEGER NOT NULL,
	status      TEXT NOT NULL DEFAULT 'in_progress'
	            CHECK (status IN ('in_progress', 'submitted', 'completed', 'abandoned')),
	submission  TEXT NOT NULL DEFAULT '',
	review_note TEXT NOT NULL DEFAULT '',
	created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
	updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
-- 同一用户在完成某个任务前，无论以哪种方式都不能再接取同一个任务
CREATE UNIQUE INDEX assignments_one_active ON assignments(user_id, task_id)
	WHERE status IN ('in_progress', 'submitted');
CREATE INDEX assignments_user_id ON assignments(user_id);
CREATE INDEX assignments_reviewer ON assignments(reviewer_id, status);

-- 积分流水，与 users.points 在同一事务中更新
CREATE TABLE points_log (
	id            INTEGER PRIMARY KEY,
	user_id       INTEGER NOT NULL REFERENCES users(id),
	delta         INTEGER NOT NULL,
	reason        TEXT NOT NULL CHECK (reason IN ('complete', 'abandon')),
	assignment_id INTEGER NOT NULL REFERENCES assignments(id),
	created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX points_log_user_id ON points_log(user_id);
`,
	// #2 管理员的切换身份账号：parent_id 指向管理员，每种角色最多一个。
	// 这些账号没有可用密码（password_hash 为 '!'），只能由管理员切换进入。
	`
ALTER TABLE users ADD COLUMN parent_id INTEGER REFERENCES users(id);
CREATE UNIQUE INDEX users_identity ON users(parent_id, role) WHERE parent_id IS NOT NULL;
`,
	// #3 授权改为绑定：普通用户申请，高级用户同意。强制解绑后两个账号进入黑名单，不能再绑定。
	`
CREATE TABLE bindings (
	member_id  INTEGER NOT NULL REFERENCES users(id),
	super_id   INTEGER NOT NULL REFERENCES users(id),
	status     TEXT NOT NULL CHECK (status IN ('pending_bind', 'bound', 'pending_unbind')),
	created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')), -- 申请绑定时间
	bound_at   TEXT,                                                          -- 绑定成功时间
	PRIMARY KEY (member_id, super_id)
) WITHOUT ROWID;
CREATE INDEX bindings_super_id ON bindings(super_id, status);
INSERT INTO bindings (member_id, super_id, status, created_at, bound_at)
	SELECT member_id, super_id, 'bound', created_at, created_at FROM authorizations;
DROP TABLE authorizations;

-- 不可绑定的账号对，与角色无关：任意一方升级或降级后仍然不能绑定。user_a < user_b。
CREATE TABLE bind_blacklist (
	user_a     INTEGER NOT NULL REFERENCES users(id),
	user_b     INTEGER NOT NULL REFERENCES users(id),
	blocked_by INTEGER NOT NULL REFERENCES users(id), -- 执行强制解绑的一方
	created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
	PRIMARY KEY (user_a, user_b),
	CHECK (user_a < user_b)
) WITHOUT ROWID;

-- 向管理员申请解除不可绑定状态
CREATE TABLE unblock_requests (
	id           INTEGER PRIMARY KEY,
	user_a       INTEGER NOT NULL,
	user_b       INTEGER NOT NULL,
	requester_id INTEGER NOT NULL REFERENCES users(id),
	status       TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected')),
	created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE UNIQUE INDEX unblock_requests_pending ON unblock_requests(user_a, user_b) WHERE status = 'pending';
`,
	// #4 高级用户是否已经看到绑定/解绑申请，用于 Member管理 的红点提醒
	`
ALTER TABLE bindings ADD COLUMN seen INTEGER NOT NULL DEFAULT 1 CHECK (seen IN (0, 1));
`,
	// #5 接取记录：新增“已取消”状态（派发者取消），收到派发任务的红点（seen），
	// 以及执行人/派发者各自从“任务记录”中删除（隐藏）记录。需要重建表以修改 status 的 CHECK。
	`
CREATE TABLE assignments_new (
	id              INTEGER PRIMARY KEY,
	task_id         INTEGER NOT NULL,
	task_public     INTEGER NOT NULL CHECK (task_public IN (0, 1)),
	user_id         INTEGER NOT NULL REFERENCES users(id),
	reviewer_id     INTEGER NOT NULL REFERENCES users(id),
	source          TEXT NOT NULL CHECK (source IN ('claim', 'draw', 'dispatch')),
	description     TEXT NOT NULL,
	points          INTEGER NOT NULL,
	status          TEXT NOT NULL DEFAULT 'in_progress'
	                CHECK (status IN ('in_progress', 'submitted', 'completed', 'abandoned', 'cancelled')),
	submission      TEXT NOT NULL DEFAULT '',
	review_note     TEXT NOT NULL DEFAULT '',
	created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
	updated_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
	seen            INTEGER NOT NULL DEFAULT 1 CHECK (seen IN (0, 1)),            -- 执行人是否已看到（派发任务）
	user_hidden     INTEGER NOT NULL DEFAULT 0 CHECK (user_hidden IN (0, 1)),     -- 执行人已删除记录
	reviewer_hidden INTEGER NOT NULL DEFAULT 0 CHECK (reviewer_hidden IN (0, 1))  -- 派发者已删除记录
);
INSERT INTO assignments_new (id, task_id, task_public, user_id, reviewer_id, source, description, points,
	status, submission, review_note, created_at, updated_at)
	SELECT id, task_id, task_public, user_id, reviewer_id, source, description, points,
	       status, submission, review_note, created_at, updated_at FROM assignments;
DROP TABLE assignments;
ALTER TABLE assignments_new RENAME TO assignments;
CREATE UNIQUE INDEX assignments_one_active ON assignments(user_id, task_id)
	WHERE status IN ('in_progress', 'submitted');
CREATE INDEX assignments_user_id ON assignments(user_id);
CREATE INDEX assignments_reviewer ON assignments(reviewer_id, status);
`,
	// #6 专有积分：高级用户派发的任务完成后获得该高级用户的专有积分，按 (普通用户, 高级用户) 分开记。
	// users.points 为通用积分。exclusive_super_id / super_id 为空表示通用积分。历史记录保持通用。
	`
ALTER TABLE assignments ADD COLUMN exclusive_super_id INTEGER REFERENCES users(id);
ALTER TABLE points_log ADD COLUMN super_id INTEGER REFERENCES users(id);
CREATE TABLE exclusive_points (
	user_id  INTEGER NOT NULL REFERENCES users(id),
	super_id INTEGER NOT NULL REFERENCES users(id),
	points   INTEGER NOT NULL DEFAULT 0, -- 可以为负数
	PRIMARY KEY (user_id, super_id)
) WITHOUT ROWID;
CREATE INDEX exclusive_points_super_id ON exclusive_points(super_id);
`,
	// #7 积分商城：owner_id 为空是官方商城（管理员维护，消耗通用积分），否则属于该高级用户的专有商城
	// （消耗该高级用户的专有积分）。商品删除只做标记，购买记录保存名称、详情、积分的快照。
	`
CREATE TABLE products (
	id         INTEGER PRIMARY KEY,
	owner_id   INTEGER REFERENCES users(id),
	name       TEXT NOT NULL,
	detail     TEXT NOT NULL DEFAULT '',
	cost       INTEGER NOT NULL CHECK (cost >= 1),
	deleted    INTEGER NOT NULL DEFAULT 0 CHECK (deleted IN (0, 1)),
	created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
	updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX products_owner_id ON products(owner_id);

CREATE TABLE purchases (
	id          INTEGER PRIMARY KEY,
	product_id  INTEGER NOT NULL REFERENCES products(id),
	user_id     INTEGER NOT NULL REFERENCES users(id),
	super_id    INTEGER REFERENCES users(id), -- 为空表示官方商城
	name        TEXT NOT NULL,
	detail      TEXT NOT NULL DEFAULT '',
	cost        INTEGER NOT NULL,
	reminded    INTEGER NOT NULL DEFAULT 0 CHECK (reminded IN (0, 1)), -- 高级用户催核销，普通用户查看后清除
	redeemed_at TEXT,
	created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX purchases_user_id ON purchases(user_id, super_id);
CREATE INDEX purchases_super_id ON purchases(super_id);
`,
	// #8 官方商城的商品可以免费（cost = 0），专有商城仍至少 1 积分。需要重建表以修改 CHECK。
	`
CREATE TABLE products_new (
	id         INTEGER PRIMARY KEY,
	owner_id   INTEGER REFERENCES users(id),
	name       TEXT NOT NULL,
	detail     TEXT NOT NULL DEFAULT '',
	cost       INTEGER NOT NULL CHECK (cost >= 1 OR (owner_id IS NULL AND cost = 0)),
	deleted    INTEGER NOT NULL DEFAULT 0 CHECK (deleted IN (0, 1)),
	created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
	updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
INSERT INTO products_new SELECT id, owner_id, name, detail, cost, deleted, created_at, updated_at FROM products;
DROP TABLE products;
ALTER TABLE products_new RENAME TO products;
CREATE INDEX products_owner_id ON products(owner_id);
`,
	// #9 买家从“已购项目”、卖家从“售出记录”中各自删除（隐藏）购买记录
	`
ALTER TABLE purchases ADD COLUMN buyer_hidden INTEGER NOT NULL DEFAULT 0 CHECK (buyer_hidden IN (0, 1));
ALTER TABLE purchases ADD COLUMN seller_hidden INTEGER NOT NULL DEFAULT 0 CHECK (seller_hidden IN (0, 1));
`,
}
