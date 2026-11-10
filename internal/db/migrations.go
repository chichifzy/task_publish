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
}
