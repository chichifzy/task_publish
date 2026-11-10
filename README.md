# 任务发布系统

Go（`net/http`）+ SQLite（`modernc.org/sqlite`，无 CGO）+ React（Vite）。
前端构建产物通过 `go:embed` 打包进可执行文件，部署只需一个可执行文件，数据库文件运行时自动创建。

## 开发

```bash
make dev       # 一键同时启动下面两个服务，Ctrl+C 一起停止
# 或分两个终端：
make dev-api   # 终端 1：Go 服务，:8080
make dev-web   # 终端 2：Vite，:5173，/api 代理到 :8080
```

访问 http://localhost:5173 。

## 管理员

系统有且仅有一个管理员，不能注册产生。首次部署时用命令行创建（随后输入密码）：

```bash
go run . -db data.db create-admin <用户ID> <用户名>
# 生产环境
./task_publish -db /path/to/data.db create-admin <用户ID> <用户名>
```

## 测试

```bash
go build -o bin/task_publish . && scripts/smoke.sh   # 后端接口冒烟测试
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
main.go, admin.go       入口、命令行参数、create-admin
internal/db/            打开数据库、迁移（migrations.go 追加新迁移）
internal/server/        路由与接口：auth / tasks / assignments / users
web/src/pages/          各页面：公共仓库、私有仓库、我的任务、审核、设置、用户管理
web/src/components/     任务卡片、随机抽取等组件
scripts/smoke.sh        后端冒烟测试
```

## 业务规则速查

- 角色：管理员（唯一）/ 高级用户（管理员授予、可撤销）/ 普通用户（注册默认）。
- 任务编号：公共任务 `G-n`，私有任务 `P-n`。
- 公共任务可见性：管理员看全部；高级用户看启用的；普通用户看启用且全体可见的。
- 普通用户获取任务：主动接取（全体可见）、随机抽取（仅管理员和高级用户可见，先看积分再接取）、被派发。
- 同一用户在完成某任务前不能以任何方式重复接取（数据库唯一索引保证）。
- 派发：只能派发私有仓库任务；管理员需对方开启“接受管理员派发任务”，高级用户需对方授权。
- 审核：派发的任务由派发者审核，公共任务由管理员审核；不通过退回进行中。
- 积分：初始 20；完成加任务积分；放弃扣任务积分的 50%（向下取整）；积分为负时不能放弃。
- 高级用户私有任务积分 1-9；复制来的公共任务可保留原积分，修改积分时需改为 1-9。
- 撤销高级用户：私有仓库冻结（只读）、已派发任务有效且仍由其审核、授权保留但不能派发；重新授予后恢复。
