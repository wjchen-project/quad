# AGENTS.md

## 项目概况

- 单二进制 Go 项目（module `todo_helper`）：`serve` 提供 HTTP API，其余命令是 CLI 客户端，二者共用 `main.go` 入口与 `internal/cli` 分发。
- 依赖方向：`cli → client → HTTP API → store`；`serve` 走 `cli → api → store`。`config`、`auth`、`bootstrap` 被两边共享。
- 默认路径：配置 `~/.todo_helper/server.json`，数据库 `~/.todo_helper/todo_helper.db`，登录态 `~/.todo_helper/session.json`。`TODO_HELPER_HOME` 覆盖整个目录，测试依赖它。
- 默认监听 `127.0.0.1:8787`，客户端默认同址；已初始化 git（默认分支 `main`），暂无 CI 与 lint 配置。

## 常用命令

```bash
go run . serve                 # 启动服务端（首次打印管理员随机密码）
go run . login -u sysadmin     # 客户端登录
go build -o todo_helper .      # 构建（*.exe / todo_helper 已被 .gitignore 忽略）
go vet ./...
go test ./...
go test ./internal/api -run TestTodoLifecycleAndIsolation -v   # 单测/单流程
```

- 测试用 `t.TempDir()` 建独立 SQLite 并起 `httptest`，无外部服务；PBKDF2 210k 迭代使整套测试需要数秒。
- 改环境变量相关逻辑后建议 `go test -count=1 ./...`，避免测试缓存掩盖问题。

## 关键约束

- SQLite 驱动固定为 `modernc.org/sqlite`（纯 Go，无 cgo）。不要换成 `mattn/go-sqlite3`；`store.Open` 的 `SetMaxOpenConns(1)` 是刻意为之，用于规避 SQLite 并发写锁。
- 表结构在 `internal/store/store.go` 的 `schemaStatements`，只有幂等 `CREATE ... IF NOT EXISTS`，没有版本化迁移。**改已有表必须补 `ALTER TABLE` 迁移**，否则老库不会更新。
- 象限存储值唯一合法写法是 `not_urgent_not_important`。别名与中文标签集中在 `internal/store/values.go`，API 只返回规范值。
- 待办状态规则：`status=done` 强制 `progress=100` 并写 `completed_at`；离开 `done` 清空 `completed_at`；仅改进度到 100 不算完成。
- 认证：访问令牌是 HS256 JWT（`internal/auth`），密钥在数据库 `meta` 表；刷新令牌只存 SHA-256 摘要并轮换，旧令牌重放会撤销该用户全部刷新令牌。密码哈希用标准库 `crypto/pbkdf2`，不要引入 bcrypt。
- 每次请求都从数据库重新加载用户，所以改角色即时生效；重置密码只撤销刷新令牌，访问令牌仍可用到过期。
- 管理员引导逻辑在 `internal/bootstrap`：仅首次建库时创建/提升管理员并打印一次密码；`serve --reset-admin-password` 用于找回。最后一个管理员不可降级或删除，也不可删除当前账号——这些约束在 `internal/api/user_handlers.go`。
- 待办严格按 JWT 用户隔离，管理员也不能跨用户读待办（用户管理是另一组接口）。
- CLI 解析统一用 `parseFlags(fs, args, allowPositional)`：它会把位置参数移到末尾，支持 `todo update 3 -p 60` 这种写法。不要直接调用 `fs.Parse`，否则解析会停在 ID 处。
- 同一参数的短/长选项（如 `-q`/`--quadrant`）绑定同一变量；判断“是否显式传入”时用 `fs.Visit` 并同时检查两个名字（见 `runTodoUpdate`）。
- 客户端遇到 401 会自动刷新并重试一次，刷新失败则删除本地登录态并返回 `client.ErrLoginRequired`。新增 API 方法请复用 `Client.do`，不要绕过刷新逻辑。
- API 错误统一 `{"error":{"code","message"}}`，用 `writeError` 输出；成功列表用 `{"users":[...]}` / `{"todos":[...]}` 包裹。

## 修改时的检查清单

- 改 API/CLI 行为时同步更新 `README.md` 的命令与接口表。
- 改动认证、用户隔离、完成时间规则后跑 `go test ./...`，并至少跑一次真实二进制冒烟：`serve` + `login` + `todo add/list/done`。
- 时间一律存 UTC 毫秒整数，JSON 输出为 RFC3339，终端按本地时区显示。
