# 陶瓷窑保温计时（kiln-hold）

陶瓷窑进入保温段后，值守员通过页面创建一次倒计时：窑位标签 + 保温分钟数。
服务端把时刻持久化到 SQLite，浏览器刷新或 API 重启后仍读取同一不可变截止时刻，
绝不重新起算，避免整炉制品被提前或延后出窑。

页面右上角的「窑位看板」入口供值班长交接时在同一页面掌握所有窑位的保温进度：
看板列出数据库中的全部计时，点选任一记录即进入原有单计时详情
（id 写入原 localStorage 键，刷新、API 重启及返回详情都落到同一不可变计时）。

## 计时规则

1. 表单只接受**非空窑位标签**（首尾空白不计，最长 80 字符）和 **1 至 180 的十进制整数分钟数**
   （拒绝小数、科学计数、字符串、越界值；前端与服务端双重校验）。
2. API 以**成功写入 SQLite 的 UTC 毫秒时刻**作为 `accepted_at`，并在同一条 `INSERT`
   中**一次性写入** `deadline = accepted_at + 分钟数 × 60000`。
3. `accepted_at` 与 `deadline` 创建后**不可修改**（无修改接口，数据库触发器兜底拒绝更新）。
4. 读取状态只按 **API 当前 UTC 毫秒** `now` 判断：
   - `now < deadline` → `HOLDING`
   - `now >= deadline` → `READY`（**临界毫秒归 READY**）
   - `remaining_ms = max(0, deadline - now)`
5. **写库失败必须返回错误**（HTTP 500 + `{"error": ...}`），且响应中**不得给出任何计时标识**。
6. 页面依据 API 返回的当前时刻 `now` 校准本地倒计时（`offset = now − 本地请求中点`）；
   刷新或 API 重启后按 localStorage 中的计时 id 读回同一截止时刻。
7. 浏览器时钟与服务端相差**超过 30000 毫秒**时页面提示偏差，但**不改变状态判定**
   （倒计时始终按校准后的服务端时刻推进）。

## 接口说明

所有时刻字段均为 UTC 毫秒（int64）。错误响应统一为 `{"error": "..."}`，绝不携带 `id`。

### `POST /api/timers` — 创建计时

请求体：

```json
{ "label": "窑位A-3", "minutes": 45 }
```

- `label`：非空字符串，首尾空白会被去除，最长 80 字符。
- `minutes`：1–180 的十进制整数（JSON number，拒绝 `1.5`、`"5"`、`1e2`）。

成功：`201 Created`

```json
{
  "id": 1,
  "label": "窑位A-3",
  "minutes": 45,
  "accepted_at": 1757923200123,
  "deadline": 1757925900123,
  "now": 1757923200130,
  "status": "HOLDING",
  "remaining_ms": 2699993
}
```

失败：`400`（校验不通过）/ `500`（写库失败），响应体只有 `{"error": "..."}`。

### `GET /api/timers/{id}` — 读取单个计时

`200` 返回与创建相同的结构，`now` / `status` / `remaining_ms` 按读取当下的
API 当前 UTC 毫秒现算；`accepted_at`、`deadline` 恒为创建时写入的值。
不存在返回 `404`。

### `GET /api/timers` — 列出全部计时

`200` 返回计时状态数组（同一 `now` 采样），默认按创建顺序（id 升序）。

附加可选参数 `?view=board` 时返回看板视图（仍以同一个服务端 `now` 计算）：
保温中的记录按截止时刻升序排在已到时记录之前，同组截止时刻相同则按 id 升序；
该确定性顺序由 SQLite 查询与 Go 服务共同保证。未带参数时响应与创建顺序不变。

### `GET /api/health` — 健康检查

`200 {"ok": true}`；数据库不可用返回 `503`。

## 快速开始（Docker Compose）

只启动 `frontend` 与 `api` 两个服务，SQLite 落在命名卷 `kiln-data` 上承担重启续作：

```bash
docker compose up --build
# 前端 http://localhost:8080 （API 直连 http://localhost:8081）
```

宿主端口可用环境变量覆盖：

```bash
WEB_PORT=9000 API_PORT=9001 docker compose up --build
```

前端容器内保留源码与依赖，可直接进入运行测试：

```bash
docker compose exec frontend npm run test       # Vitest
docker compose exec frontend npm run typecheck  # vue-tsc
```

验证重启不漂移：创建计时后执行 `docker compose restart api`，
刷新页面，截止时刻保持不变。

## 本地开发

```bash
# API（默认 :8080，SQLite 文件 ./kiln.db；ADDR / DB_PATH 可覆盖）
cd api && go run .

# 前端（:5173，/api 代理到 localhost:8080；API_PORT 可覆盖代理目标）
cd web && npm install && npm run dev
```

## 测试

```bash
# Go：临界规则、校验、不可变性、重启续作、写库失败无标识
cd api && go test ./...

# Vitest：前端状态/剩余毫秒/偏差阈值/格式化等纯逻辑
cd web && npm run test
# 或在前端容器内运行：
docker compose exec frontend npm run test

# Playwright：真实联调（创建→递减→刷新→API 重启→临界翻转→偏差提示→窑位看板）
docker compose up -d --build
cd e2e && npm install && npx playwright install chromium
npm run test          # WEB_URL 默认 http://localhost:8080
```

非 Docker 环境跑联调：先 `cd api && go run .`，再 `cd web && npm run dev`，
然后 `cd e2e && WEB_URL=http://localhost:5173 npm run test`
（「API 重启后截止时刻不漂移」用例在无 docker 时自动跳过）。

## 目录结构

```
api/    Go API（SQLite 持久化，modernc.org/sqlite 纯 Go 驱动）
web/    Vue 3 + Vite 前端（容器内 vite preview 托管构建产物并反代 /api）
e2e/    Playwright 联调测试
docker-compose.yml
```
