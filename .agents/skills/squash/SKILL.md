---
name: squash
description: "将当前分支 squash 合并到 master 并提交。用法：/squash。当用户说 /squash 或 'squash merge' 时触发。**只在非 master 分支上运行**。"
---

# Squash Merge to Master

## 前置检查

**如果当前分支是 master，拒绝执行并提示用户。**

运行 `git branch --show-current` 检查当前分支，如果是 master，直接退出。

## 流程

### 1. 保存当前分支名

```bash
CURRENT_BRANCH=$(git branch --show-current)
```

### 2. 切换到 master

```bash
git checkout master
```

如果失败（如有未提交的改动），提示用户先处理。

### 3. 拉取最新 master

```bash
git pull origin master
```

### 4. Squash 合并原分支

```bash
git merge --squash $CURRENT_BRANCH
```

这会将改动暂存但不提交。

### 5. 调用 /commit skill

使用 Skill tool 调用 commit skill 来完成提交和推送：

```
skill: "commit"
```

commit skill 会：
- 总结暂存的改动
- 生成中文 commit message（不含 Co-Authored-By）
- 执行 commit
- push 到远程

### 6. 清理原分支（可选）

如果 commit 成功，询问用户是否删除原分支：

```bash
git branch -d $CURRENT_BRANCH
git push origin --delete $CURRENT_BRANCH
```
