# 本地 API

- `GET /api/dashboard`：运行态势
- `POST /api/adapters`：登记协议适配器
- `POST /api/points`：登记统一测点
- `POST /api/rules`：创建本地阈值规则
- `POST /api/config`：校验并应用配置修订
- `POST /api/ingest`：适配器写入归一前原始值
- `POST /api/cloud-link`：模拟/更新云端链路
- `POST /api/outbox/drain`：模拟云端批量接收和 ACK

所有 `/api` 请求使用 `x-api-key`。
