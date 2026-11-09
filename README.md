# 任务发布系统

Go（`net/http`）+ SQLite（`modernc.org/sqlite`，无 CGO）+ React（Vite）。
前端构建产物通过 `go:embed` 打包进可执行文件，部署只需一个可执行文件，数据库文件运行时自动创建。

## 开发

```bash
make dev-api   # 终端 1：Go 服务，:8080
make dev-web   # 终端 2：Vite，:5173，/api 代理到 :8080
```

访问 http://localhost:5173 。

## 管理员

注册账号后，用命令行提升为管理员：

```bash
go run . -db data.db promote <username>
# 或生产环境
./task_publish -db /path/to/data.db promote <username>
```

## 构建与部署

```bash
make build         # 本机版本 → bin/task_publish
make build-linux   # Linux amd64 → bin/task_publish-linux-amd64
```

```bash
./task_publish -addr :8080 -db /var/lib/task_publish/data.db
```

HTTPS 部署（例如放在 Caddy/Nginx 后面）时加上 `-secure-cookie`。
备份：`sqlite3 data.db ".backup backup.db"`，或停服后直接复制 `data.db`。

## 目录

```
main.go                 入口、命令行参数
internal/db/            打开数据库、迁移（migrations.go 追加新迁移）
internal/server/        路由、认证、任务接口
web/                    React 前端；embed.go 嵌入 dist
```

## API

| 方法 | 路径 | 权限 |
|---|---|---|
| POST | /api/auth/register, /api/auth/login, /api/auth/logout | 公开 |
| GET | /api/me | 登录 |
| GET | /api/tasks | 登录（管理员看全部） |
| POST | /api/tasks/{id}/claim | 登录 |
| POST | /api/admin/tasks | 管理员 |
| PUT / DELETE | /api/admin/tasks/{id} | 管理员 |
