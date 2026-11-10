.PHONY: dev dev-api dev-web web build clean

# 开发：make dev 同时启动前后端（或两个终端分别运行 make dev-api 和 make dev-web），然后访问 http://localhost:5173
# 同一局域网的手机访问 http://<电脑IP>:5173（vite.config.ts 中 host: true，Vite 启动时会打印 Network 地址）
# 前后端放在同一个 shell 里，Ctrl+C 时由 trap 正常退出，make 不再报 Error / Interrupt
dev: web/dist/index.html
	@go build -o bin/dev-server .
	@trap 'exit 0' INT TERM; \
	./bin/dev-server -addr :8080 -db data.db & \
	(cd web && exec ./node_modules/.bin/vite) & \
	wait

# 不用 go run / pnpm dev：它们在 Ctrl+C 时返回非 0，make 会报 Error
dev-api: web/dist/index.html
	go build -o bin/dev-server . && exec ./bin/dev-server -addr :8080 -db data.db

dev-web:
	cd web && exec ./node_modules/.bin/vite

web:
	cd web && pnpm install --frozen-lockfile && pnpm build

# 生产构建：前端产物嵌入可执行文件，输出 bin/task_publish
build: web
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/task_publish .

# 交叉编译 Linux 服务器版本
build-linux: web
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o bin/task_publish-linux-amd64 .

# go:embed 要求 dist 目录存在；开发时放一个占位页
web/dist/index.html:
	mkdir -p web/dist
	echo '<p>前端开发请访问 <a href="http://localhost:5173">http://localhost:5173</a></p>' > web/dist/index.html

clean:
	rm -rf bin web/dist
