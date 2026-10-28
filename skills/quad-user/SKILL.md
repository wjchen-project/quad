---
name: quad-user
description: 当用户要求新建、查看、改密、改角色或删除用户，调用 /api/v1/users 接口，处理管理员引导或密码重置，或修改认证、角色与管理员约束时使用。
metadata:
  opencode/autoinvoke: true
---

# quad-user：用户与权限域

本技能覆盖 quad 的用户管理、角色与认证约束。用户管理接口**仅管理员可用**，
待办数据本身与用户管理无关（待办见 `quad-todo` 技能）。

## 触发场景

- 用户说"建个账号 / 看看有哪些用户 / 重置密码 / 给他管理员 / 删掉这个用户"
- 需要调用 `/api/v1/users*`
- 涉及管理员引导（首次启动）、`--reset-admin-password`、角色即时生效、
  密码哈希、令牌轮换与撤销

## 数据模型

字段见 `internal/store/models.go` 的 `store.User`：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | int64 | 自增主键 |
| `username` | string | 唯一，大小写不敏感匹配；3-32 位字母、数字或 `_ . -` |
| `role` | string | `admin` 或 `user` |
| `created_at` / `updated_at` | time | UTC 毫秒，JSON 为 RFC3339 |

`password_hash` 只在库内，不出现在任何接口响应中。CLI 位置参数支持
`<用户名|ID>`，由 `internal/cli/user.go` 的 `resolveUser` 解析：先按数字 ID 精确匹配，
再按用户名大小写不敏感匹配，都找不到时报"用户不存在"。

## 不可违反的规则

1. **权限门禁**：所有 `/api/v1/users*` 都挂 `adminOnly` 中间件，非管理员返回
   `403 forbidden`。改路由时不要漏挂。
2. **最后一个管理员不可降级或删除**（`CountAdmins <= 1` 时返回 `409 conflict`）。
3. **不能删除当前登录账号**（`id == current.ID` 时返回 `409 conflict`）。
4. **改密会撤销该用户全部刷新令牌**（`RevokeUserRefreshTokens`），但已签发的
   访问令牌仍可用到过期（默认 30 分钟）。不要改成"改密后访问令牌立即失效"。
5. **角色变更即时生效**：每个请求都从数据库重新加载用户，不要加角色缓存。
6. **先校验再写库**：`handleUpdateUser` 会先校验 role/password 全部字段，
   再执行 `SetUserRole` 与 `SetUserPassword`，避免部分更新。保持这个顺序。
7. **用户名规则**：`^[A-Za-z0-9_.-]{3,32}$`；**密码至少 8 位、最多 128 位**。
   重复用户名返回 `409 conflict`（唯一索引冲突映射为 `store.ErrConflict`）。
8. **密码哈希用标准库 `crypto/pbkdf2`**（PBKDF2-HMAC-SHA256，210,000 迭代，
   16 字节随机盐）。不要引入 bcrypt，也不要降低迭代次数。
9. **删除用户会级联删除其待办与刷新令牌**（外键 `ON DELETE CASCADE`）。
   如果改这个行为，必须同时改 `internal/store/store.go` 的建表语句。
10. **CLI 解析**：`user passwd/rm` 的位置参数是单个 `<用户名|ID>`，
    用 `parseFlags(fs, args, true)`；`user role` 需要两个位置参数
    （`<用户名|ID> <admin|user>`）。不要直接调 `fs.Parse`。
11. **错误响应**：统一 `{"error":{"code","message"}}`；成功列表用 `{"users":[...]}` 包裹。

## 使用 CLI 管理用户

```bash
quad user list
quad user add -u alice                       # 不传 -p 则随机生成并打印
quad user add -u bob -p 'S3cret!!' -r admin
quad user passwd alice -p 'NewPass123'       # 也可用 ID：quad user passwd 2
quad user role bob admin
quad user rm bob
```

注意：

- `user add` 不指定 `-p` 时生成 16 位随机密码并打印一次，需提醒用户尽快修改。
- `user passwd` 作用于自己时，CLI 会提示刷新令牌已失效。
- 随机密码字母表去掉了易混淆字符（0/O/1/l/I），见 `internal/auth/password.go`。

## 调用 HTTP API

前缀 `/api/v1`，认证头 `Authorization: Bearer <access_token>`。

| 方法 | 路径 | 权限 | 说明 |
| --- | --- | --- | --- |
| GET | `/api/v1/users` | 管理员 | 用户列表 |
| POST | `/api/v1/users` | 管理员 | 新建，返回 201 |
| PATCH | `/api/v1/users/{id}` | 管理员 | 改密码或角色，二者至少一个 |
| DELETE | `/api/v1/users/{id}` | 管理员 | 删除，返回 204 |

PATCH 请求体：`{"password": "...", "role": "admin"}`，字段均为指针，未传不动。

状态码：`400` 参数错误、`401` 未认证/令牌失效、`403` 非管理员、
`404` 用户不存在、`409` 冲突（重名、最后一个管理员、删除当前账号）。

## 认证与管理员引导

- 访问令牌：HS256 JWT（`internal/auth/token.go`），密钥随机生成后存数据库 `meta` 表。
- 刷新令牌：随机串，只存 SHA-256 摘要，每次刷新轮换；**旧令牌重放会撤销该用户
  全部刷新令牌**。
- 首次启动由 `internal/bootstrap` 创建管理员（默认用户名 `sysadmin`）并打印一次
  随机密码；忘记密码用 `quad serve --reset-admin-password`。

## 改代码时的检查清单

1. 改 API/CLI 行为时同步更新 `README.md` 的命令与接口表。
2. 改动认证、用户隔离、管理员约束后跑 `go test ./...`，并做一次真实二进制冒烟：
   `serve` + `login` + `user add` + `user list`。
3. 相关文件：`internal/cli/user.go`、`internal/cli/auth.go`、
   `internal/api/user_handlers.go`、`internal/api/server.go`、
   `internal/store/users.go`、`internal/auth/`、`internal/bootstrap/`。
