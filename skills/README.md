# skills

本目录存放 OpenCode 项目级技能（skills），按 quad 的功能域拆分。

目录已在仓库根的 `opencode.json` 中通过 `skills` 数组注册：

```jsonc
{
  "skills": [
    "./skills"
  ]
}
```

相对路径基于 OpenCode 的工作目录解析，因此从仓库任意子目录启动都能被发现。

## 约定

- 一个目录就是一个技能，入口文件必须命名为 `SKILL.md`（大小写敏感）。
- 技能 ID 取自目录名，需为小写 kebab-case，并与目录名保持一致。
- frontmatter 至少写清 `name` 与 `description`；`description` 是模型判断
  "什么时候该加载这个技能"的唯一依据，要写清触发场景。
- 相对路径写入 `SKILL.md` 时，基于该文件所在目录解析。
- 相关脚本、参考文件放在同技能的 `scripts/`、`references/` 子目录下，
  不会被 OpenCode 当成新的技能扫描。

## 现有技能

| ID | 覆盖范围 | 相关源码 |
| --- | --- | --- |
| `quad-todo` | 待办（四象限）的 CLI 用法、HTTP API、状态与进度规则 | `internal/cli/todo.go`、`internal/api/todo_handlers.go`、`internal/store/todos.go` |
| `quad-user` | 用户与权限：CLI 用法、管理员 API、认证与管理员约束 | `internal/cli/user.go`、`internal/api/user_handlers.go`、`internal/auth`、`internal/bootstrap` |

## 新增技能

1. 新建 `skills/<skill-id>/SKILL.md`。
2. frontmatter 写 `name`、`description`，需要时加
   `metadata: { opencode/autoinvoke: false }` 让它只被显式调用。
3. 在 `SKILL.md` 中说明触发场景、不可违反的规则、CLI/API 用法与相关源码位置。
4. 在上表登记一行。
