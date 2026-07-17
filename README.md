# VenueFlow Admin

VenueFlow 是场馆服务调度后台与 Go Gin API。它不是静态看板，而是把“创建活动订单 → 分配场馆工作人员 → 现场服务 → 送达 → 签收 → 场地事件处理 → 结算确认”串成可追踪、可重试的场馆服务流程。

## 运营能力

- 活动订单：分页查询、创建、分配场馆工作人员、推进现场服务状态、查看事件时间线。
- 场地事件：按状态查询、处理场地事件，处理结果会落库。
- 结算：读取周期账单、确认待结算账单。
- 可靠性：所有写接口要求 `Idempotency-Key`；Redis 8 用于幂等结果和分布式锁，MySQL 8.4 保存业务数据与事件。
- 统一响应：`{ "code": 0, "message": "ok", "data": {}, "traceId": "..." }`。

状态流转：`待调度 → 待接单 → 现场服务中 → 已送达 → 已完成`，允许从未完成状态取消，非法回退会返回 HTTP 409。

## 一键运行（MySQL 8.4 + Redis 8）

```bash
docker compose -f deploy/docker-compose.yml up --build
```

API 默认监听 `http://localhost:8080`，初始化脚本会创建表、索引和演示数据。环境变量：`MYSQL_DSN`、`REDIS_ADDR`、`REDIS_PASSWORD`、`HTTP_ADDR`。本地不配置数据库和 Redis 时，API 会使用内存演示存储，便于开发和单测。

## 前端运行

```bash
cd web
npm install
npm run dev
```

Vite 已代理 `/api` 到 `http://localhost:8080`。如果前后端不在同一地址，可设置 `VITE_API_BASE=http://localhost:8080/api/v1`。

## curl 验证流程

```bash
curl http://localhost:8080/healthz
curl http://localhost:8080/api/v1/shipments?page=1\&pageSize=20
curl -X POST http://localhost:8080/api/v1/shipments -H 'Content-Type: application/json' -H 'Idempotency-Key: demo-create-001' -d '{"route":"浦东 → 静安","cargo":"生鲜 2 箱","eta":"16:30"}'
curl -X POST http://localhost:8080/api/v1/shipments/FF-260716-017/assign -H 'Content-Type: application/json' -H 'Idempotency-Key: demo-assign-001' -d '{"driver":"周师傅"}'
curl -X POST http://localhost:8080/api/v1/shipments/FF-260716-017/status -H 'Content-Type: application/json' -H 'Idempotency-Key: demo-status-001' -d '{"status":"现场服务中","note":"场馆工作人员已出发"}'
curl -X POST http://localhost:8080/api/v1/exceptions/EX-041/resolve -H 'Idempotency-Key: demo-exception-001'
curl -X POST http://localhost:8080/api/v1/settlements/SET-2026-07-15/confirm -H 'Idempotency-Key: demo-settlement-001'
```

## 校验

```bash
go test ./...
go vet ./...
cd web && npm test && npm run build
```

所有数据为虚构演示数据；项目不接入真实用户定位、支付或身份证信息。
