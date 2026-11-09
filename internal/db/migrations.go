package db

// migrations 按顺序执行，每条只执行一次。
// 修改表结构时在末尾追加新的一条，不要改动已发布的旧条目。
var migrations = []string{
	// #1 初始表结构
	`
CREATE TABLE users (
	id            INTEGER PRIMARY KEY,
	username      TEXT NOT NULL UNIQUE COLLATE NOCASE,
	password_hash TEXT NOT NULL,
	role          TEXT NOT NULL DEFAULT 'user' CHECK (role IN ('user', 'admin')),
	created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

-- 只保存 token 的 SHA-256，数据库泄露也无法直接冒用会话
CREATE TABLE sessions (
	token_hash TEXT PRIMARY KEY,
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	expires_at INTEGER NOT NULL -- Unix 秒
);
CREATE INDEX sessions_user_id ON sessions(user_id);

CREATE TABLE tasks (
	id          INTEGER PRIMARY KEY,
	title       TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	status      TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'closed')),
	created_by  INTEGER REFERENCES users(id) ON DELETE SET NULL,
	created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
	updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

-- 接取记录。task_id 唯一 = 一个任务只能被一人接取；
-- 以后允许多人接取时，改成 UNIQUE (task_id, user_id)。
CREATE TABLE task_claims (
	task_id    INTEGER NOT NULL UNIQUE REFERENCES tasks(id) ON DELETE CASCADE,
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	claimed_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX task_claims_user_id ON task_claims(user_id);
`,
}
