---
name: commit
description: "执行 git commit 并 push。用法：/commit。当用户说 /commit 时触发此 skill，自动查看当前所有改动，用中文总结并提交，然后 push 到当前分支。"
---

# Commit & Push

## 流程

### 1. 检查改动

运行 `git status` 和 `git diff HEAD` 查看所有改动的文件和内容。

- 如果没有任何改动，提示用户后退出
- **自动执行** `git add .` 添加所有改动文件

### 2. 总结改动

阅读 diff 内容，用中文简要总结改了什么（2-4 句话），格式类似：

```
当前改动：
- xxx
- xxx
```

### 3. 生成 commit message

用中文写 commit message，简洁明了，1-2 行。风格参考项目历史提交记录。

### 4. 执行 commit

运行 `git commit -m "commit message"` 提交。

- **不要**在 commit message 中添加 `Co-Authored-By`、`Reviewed-by` 或任何其他 AI/工具署名信息
- 只保留纯中文的 commit message

### 5. 处理 pre-commit 失败

如果 commit 被 pre-commit hook 拦截：

- **只有**错误信息涉及 `ruff`（Python 格式/lint）或 `prettier`（前端格式）时，才自动重试
- 自动修复后，重新 `git add .` 再执行一次 `git commit`
- 如果第二次仍然失败，把错误信息展示给用户，让用户决定
- 如果是其他类型的 hook 错误（如测试失败、自定义 hook），直接展示错误给用户，不自动重试

### 6. Push

commit 成功后，执行 `git push` 推送到当前分支。如果 push 失败，展示错误信息。
