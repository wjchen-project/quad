---
name: quad-todo
description: 当用户要求添加、查看、修改、完成或删除待办，按象限或状态筛选待办，调用 /api/v1/todos 接口，或修改待办字段、状态与进度规则时使用。
metadata:
  opencode/autoinvoke: true
---

# quad-todo：待办（四象限）域

本技能覆盖 quad 的待办功能。待办严格按 JWT 用户隔离：所有查询都带 `user_id`，
管理员也不能跨用户读别人的待办（用户管理是另一组接口，见 `quad-user` 技能）。

## 触发场景

- 用户说"加个待办 / 看看待办 / 标记完成 / 删掉这条 / 改进度 / 按象限筛选"
- 需要调用 `GET/POST/PATCH/DELETE /api/v1/todos*`
- 要修改待办字段、状态流转、进度、完成时间或四象限取值

## 数据模型

字段见 `internal/store/models.go` 的 `store.Todo`：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | int64 | 自增主键 |
| `user_id` | int64 | 归属用户，不可显式指定，服务端从 JWT 取 |
| `title` | string | 必填，1-200 字 |
| `notes` | string | 备注，最多 4000 字 |
| `quadrant` | string | 四象限，见下表 |
| `status` | string | pending / in_progress / done / cancelled |
| `progress` | int | 0-100 |
| `created_at` / `updated_at` | time | UTC 毫秒，JSON 为 RFC3339 |
| `completed_at` | *time | 仅在 done 时有值 |

象限存储值**只有**这四个，别名与中文标签集中在 `internal/store/values.go`：

| 存储值 | 别名 | 中文标签 |
| --- | --- | --- |
| `urgent_important` | `q1` | 重要紧急 |
| `important_not_urgent` | `q2` | 重要不紧急 |
| `urgent_not_important` | `q3` | 紧急不重要 |
| `not_urgent_not_important` | `q4` | 不重要不紧急 |

状态别名：`todo`/`待办` → `pending`，`doing`/`进行中` → `in_progress`，
`done`/`completed`/`已完成` → `done`，`cancelled`/`canceled`/`已取消` → `cancelled`。

## 不可违反的规则

1. **状态流转**（`internal/api/todo_handlers.go` 的 `handleUpdateTodo`）：
   - 改为 `done` 时强制 `progress = 100`，并在 `completed_at == nil` 时写入当前时间。
   - 离开 `done`（改成任何其他状态）时清空 `completed_at`。
   - **只把进度改成 100 不算完成**，不会写 `completed_at`。
   - 新建时若直接传 `status=done`，同样强制 `progress = 100` 并写 `completed_at`。
2. **象限存储值唯一合法写法是 `not_urgent_not_important`**，不要写成
   `not_important_not_urgent` 或其他变体；别名与中文标签只加在 `values.go`。
3. **部分更新**：`PATCH /api/v1/todos/{id}` 的请求体字段都是指针，未传的字段不动；
   全部为 null 时返回 400 `invalid_request`。CLI 侧用 `fs.Visit` 判断"是否显式传入"。
4. **用户隔离**：`store.GetTodo/UpdateTodo/DeleteTodo` 的 SQL 必须带 `AND user_id = ?`。
   跨用户访问一律表现为 404 `not_found`，不要返回 403 或泄露存在性。
5. **时间**：库内存 UTC 毫秒整数（`time.UnixMilli`），JSON 输出 RFC3339，
   终端按本地时区显示（`internal/cli/format.go` 的 `formatTime`）。
6. **CLI 解析**：待办 ID 是位置参数，必须走 `parseFlags(fs, args, true)`，
   它会把位置参数移到末尾，使 `todo update 3 -p 60` 可用。不要直接调 `fs.Parse`。
   同一参数短/长选项绑定同一变量，判断显式传入要同时检查两个名字。
7. **错误响应**：统一 `{"error":{"code","message"}}`，用 `writeError` 输出；
   成功列表用 `{"todos":[...]}` 包裹。
8. 客户端新增 API 方法请复用 `internal/client/client.go` 的 `Client.do`，
   不要绕过 401 自动刷新重试逻辑。

## 使用 CLI 操作待办

```bash
quad todo add -t "写周报" -q q1 -n "周五前"          # 必须指定象限
quad todo list                                       # 全部（默认 200 条）
quad todo list -s pending -q q2 --limit 50 --offset 0
quad todo show 3
quad todo update 3 -p 60 -s in_progress              # 参数可放 ID 前后
quad todo done 3                                     # 自动置 100 并记完成时间
quad todo start 3
quad todo rm 3
```

注意：

- `todo add` 的 `--title` 和 `--quadrant` 必填，缺一个直接报错。
- `--progress` 传 `-1` 表示"未提供"（add 的默认值）；update 时只有显式传了才改。
- `-n ""` 可以清空备注。
- 列表分页：`--limit` 最大 500，超过会被截断；`--offset` 非负。
- 所有子命令都要求已登录，未登录会提示 `client.ErrLoginRequired`。

## 调用 HTTP API

前缀 `/api/v1`，认证头 `Authorization: Bearer <access_token>`。

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/v1/todos` | 列表，查询参数 `status`、`quadrant`、`limit`、`offset` |
| POST | `/api/v1/todos` | 新建，返回 201 |
| GET | `/api/v1/todos/{id}` | 详情 |
| PATCH | `/api/v1/todos/{id}` | 部分更新 |
| DELETE | `/api/v1/todos/{id}` | 删除，返回 204 |

新建示例：

```json
{
  "title": "写周报",
  "notes": "周五前完成",
  "quadrant": "urgent_important",
  "status": "pending",
  "progress": 0
}
```

状态码：`400` 参数错误、`401` 未认证/令牌失效、`404` 待办不存在（含跨用户）。

## 改代码时的检查清单

1. 改字段或校验规则时，同步更新 `internal/api/validation.go`、CLI 报错文案、
   以及 `README.md` 的命令与接口表。
2. 改状态/进度/完成时间规则后跑 `go test ./...`，并做一次真实二进制冒烟：
   `serve` + `login` + `todo add/list/done`。
3. 相关文件：`internal/cli/todo.go`、`internal/cli/format.go`、
   `internal/api/todo_handlers.go`、`internal/store/todos.go`、
   `internal/store/values.go`、`internal/client/client.go`。
