# 编程学习成就系统（Go）

一个本地单机版的学习成就系统，目标是把学习行为转化为可追踪、可解锁的成就进度。

## 功能

- 今日打卡（习惯追踪）
- 学习行为记录（学习时长、技能模块、项目里程碑、Bug 修复、复盘、Git 提交）
- 自动累计 XP 与等级
- 成就分组展示：习惯、技能、作品、挑战、协作
- 铜/银/金三档解锁机制
- 本地数据持久化（`data/state.json`）

## 运行方式

```bash
go run -buildvcs=false .
```

然后访问：

`http://localhost:8080`

## 项目结构

- `main.go`：后端 API、成就判定、状态持久化
- `web/index.html`：前端页面（原生 HTML/CSS/JS）
- `data/state.json`：运行后自动生成的用户数据

## API（MVP）

- `GET /api/state`：获取当前仪表盘数据
- `POST /api/checkin`：今日打卡（每天一次）
- `POST /api/action`：记录学习行为

`/api/action` 请求体示例：

```json
{
  "kind": "skill_module",
  "amount": 1
}
```

可用 `kind`：

- `study_hour`
- `skill_module`
- `project`
- `bug_fix`
- `reflection`
- `git_commit`
