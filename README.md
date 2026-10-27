# quad

终端待办工具：**客户端与服务端共用同一个二进制**。服务端提供 HTTP API（用户管理 + 待办管理），客户端通过 CLI 操作，数据存储在 SQLite。

## 功能

- `serve` 启动 API 服务端；同一二进制提供 `login` / `todo` / `user` 等客户端命令。
- 用户管理仅管理员可用；首次启动自动创建管理员（用户名默认 `sysadmin`），随机密码只打印一次。
- 待办包含四象限优先级、标题、备注、状态、进度、创建时间、完成时间。
- 访问令牌 + 刷新令牌；登录态保存在用户目录（默认 `~/.quad/session.json`），访问令牌过期时客户端自动续期。
- SQLite 使用纯 Go 驱动（`modernc.org/sqlite`），无需 cgo / gcc。

## 快速开始

```bash
go build -o quad .

# 终端 1：启动服务端（首次会打印随机管理员密码）
./quad serve

# 终端 2：登录（也可不带 -p，交互式输入密码）
./quad login -u sysadmin -p '<首次启动打印的密码>'

./quad todo add -t "写周报" -n "周五前完成" -q q1
./quad todo list
./quad todo done 1
./quad whoami
```

服务端默认监听 `127.0.0.1:8787`，客户端默认连接同一地址；连其他地址用 `login -s http://host:port`。

## Docker 部署

```bash
docker compose up -d --build
docker compose logs quad | grep -A2 '首次启动'   # 首次启动打印管理员密码
```

宿主机安装 CLI 后连接容器：

```bash
go build -o quad .
./quad login -s http://127.0.0.1:8787 -u sysadmin
```

说明：

- 镜像多阶段构建（`golang:1.27.3-alpine` → `alpine:3.22`），以非 root 用户（uid 10001）运行，监听 `0.0.0.0:8787`，自带 `/healthz` 健康检查。
- 配置、SQLite 数据库、JWT 密钥都保存在命名卷 `quad-data` 的 `/data` 下；`docker compose down` 不会删数据，`down -v` 才会。
- 端口可用 `QUAD_PORT=9000 docker compose up -d` 覆盖；默认只绑定宿主机 `127.0.0.1`，需要远程访问时改 `ports` 并自行加 TLS 反向代理。
- 管理员用户名用 `QUAD_ADMIN_USERNAME` 覆盖。
- 若改用宿主机目录（`./data:/data`），需保证 uid 10001 可写：`sudo chown -R 10001:10001 ./data`。

## 命令

### 服务端

```bash
quad serve [--addr 127.0.0.1:8787] [--db <path>] [--config <path>] \
           [--admin-username sysadmin] [--reset-admin-password]
```

- `--reset-admin-password`：启动时重置配置的管理员密码并打印新密码（忘记密码时使用）。
- 首次启动会把默认配置写入配置文件，并在数据库中创建管理员。

### 认证

```bash
quad login [-s <server>] [-u <username>] [-p <password>]
quad logout
quad whoami
```

登录成功后 token / refresh_token 写入 `~/.quad/session.json`（权限 0600）；`logout` 会撤销服务端刷新令牌并删除本地文件。

### 待办

```bash
quad todo add    -t <标题> -q <q1..q4> [-n 备注] [-s 状态] [-p 进度]
quad todo list   [-s 状态] [-q 象限] [--limit N] [--offset N]
quad todo show   <ID>
quad todo update <ID> [-t 标题] [-n 备注] [-q 象限] [-s 状态] [-p 进度]
quad todo done   <ID>
quad todo start  <ID>
quad todo rm     <ID>
```

参数可以放在 ID 前后，例如 `todo update 3 -p 60 -s in_progress` 与 `todo update -p 60 3` 等价。

象限与状态可用别名：

| 象限 | 别名 |
| --- | --- |
| 重要紧急 | `q1` / `urgent_important` / `重要紧急` |
| 重要不紧急 | `q2` / `important_not_urgent` / `重要不紧急` |
| 紧急不重要 | `q3` / `urgent_not_important` / `紧急不重要` |
| 不重要不紧急 | `q4` / `not_urgent_not_important` / `不重要不紧急` |

| 状态 | 别名 |
| --- | --- |
| 待办 | `pending` / `todo` |
| 进行中 | `in_progress` / `doing` |
| 已完成 | `done` / `completed` |
| 已取消 | `cancelled` / `canceled` |

规则：状态改为 `done` 时进度自动置 100 并记录完成时间；从 `done` 改回其他状态会清空完成时间。

### 用户管理（管理员）

```bash
quad user list
quad user add    -u <用户名> [-p 密码] [-r user|admin]
quad user passwd <用户名|ID> [-p 新密码]
quad user role   <用户名|ID> <admin|user>
quad user rm     <用户名|ID>
```

不指定密码时自动生成并打印。最后一个管理员不能被降级或删除，也不能删除当前登录账号。

## 配置

默认数据目录 `~/.quad`，可用 `QUAD_HOME` 整体覆盖。优先级：**命令行参数 > 环境变量 > 配置文件 > 默认值**。

| 环境变量 | 说明 | 默认 |
| --- | --- | --- |
| `QUAD_HOME` | 数据目录 | `~/.quad` |
| `QUAD_CONFIG` | 服务端配置文件 | `<HOME>/server.json` |
| `QUAD_ADDR` | 监听地址 | `127.0.0.1:8787` |
| `QUAD_DB` | SQLite 路径 | `<HOME>/quad.db` |
| `QUAD_ADMIN_USERNAME` | 管理员用户名 | `sysadmin` |
| `QUAD_ACCESS_TOKEN_MINUTES` | 访问令牌有效期（分钟） | `30` |
| `QUAD_REFRESH_TOKEN_HOURS` | 刷新令牌有效期（小时） | `720` |
| `QUAD_USERNAME` | `login` 默认用户名 | `sysadmin` |

配置文件示例（`~/.quad/server.json`）：

```json
{
  "addr": "127.0.0.1:8787",
  "db_path": "C:/Users/me/.quad/quad.db",
  "admin_username": "sysadmin",
  "access_token_minutes": 30,
  "refresh_token_hours": 720
}
```

## HTTP API

所有业务接口前缀 `/api/v1`，认证使用 `Authorization: Bearer <access_token>`。

| 方法 | 路径 | 权限 | 说明 |
| --- | --- | --- | --- |
| GET | `/healthz` | 公开 | 健康检查 |
| POST | `/auth/login` | 公开 | 登录，返回 access/refresh token |
| POST | `/auth/refresh` | 公开 | 用 refresh_token 换取新令牌对（轮换） |
| POST | `/auth/logout` | 登录 | 撤销 refresh_token |
| GET | `/me` | 登录 | 当前用户 |
| GET/POST | `/todos` | 登录 | 列表 / 新建（仅本人数据） |
| GET/PATCH/DELETE | `/todos/{id}` | 登录 | 查看 / 部分更新 / 删除 |
| GET/POST | `/users` | 管理员 | 用户列表 / 新建 |
| PATCH/DELETE | `/users/{id}` | 管理员 | 改密码或角色 / 删除 |

新建待办示例：

```json
{
  "title": "写周报",
  "notes": "周五前完成",
  "quadrant": "urgent_important",
  "status": "pending",
  "progress": 0
}
```

错误响应统一为：

```json
{"error":{"code":"invalid_request","message":"quadrant 取值无效"}}
```

主要状态码：`400` 参数错误、`401` 未认证/令牌失效、`403` 非管理员、`404` 资源不存在、`409` 冲突（重名、最后一个管理员等）。

## 设计与安全说明

- 密码使用 PBKDF2-HMAC-SHA256（210,000 次迭代，随机盐）；首次管理员密码为 20 位随机字符。
- 访问令牌为 HS256 JWT（默认 30 分钟），密钥随机生成后保存在数据库 `meta` 表；刷新令牌为随机串，仅存 SHA-256 摘要，每次刷新轮换。
- 已撤销的 refresh_token 再次使用会被视为重放，该用户全部刷新令牌立即失效。
- 服务端每个请求都会重新读取用户，角色变更即时生效；重置密码只撤销刷新令牌，已签发的访问令牌最多再有效 30 分钟。
- SQLite 开启 WAL、外键，并用单连接串行化写入；库文件默认在用户目录，不随项目提交。
- 当前未内置 TLS、限流与审计日志；绑定公网地址时请放在反向代理之后。

## 开发

```bash
go test ./...        # 全部测试（临时目录 SQLite，无外部依赖）
go test ./internal/api -run TestTodoLifecycleAndIsolation -v   # 单个流程
go vet ./...
go build ./...
```

目录结构：

```
main.go                 入口
internal/cli/           serve/login/logout/whoami/todo/user 子命令与终端输出
internal/client/        HTTP 客户端、自动刷新、登录态文件
internal/api/           路由、中间件、请求校验、业务处理器
internal/store/         SQLite 表结构、用户 / 待办 / 刷新令牌 / 别名规范化
internal/auth/          密码哈希、JWT、刷新令牌
internal/config/        配置加载、环境变量覆盖、默认路径
internal/bootstrap/     首次管理员初始化、JWT 密钥、管理员密码重置
```
