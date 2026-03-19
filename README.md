# AegisPay 神盾支付

高可用、防资损、可扩展的统一支付中台，采用 DDD 分层并集成 Copilot AI 子系统（NL2SQL + 异步 RAG 风控）。

## 项目简介

AegisPay 是基于 Go 的单体支付系统，包含：
- 交易核心链路：统一下单、回调处理、订单查询、退款能力
- 商户通知链路：基于 Redis Streams 的异步 Webhook
- Copilot 子系统：商户数据问答与异步智能风控（Milvus 向量检索 + LLM 分析）

## 技术栈

| 分类 | 技术 |
|------|------|
| 语言 | Go 1.25 |
| Web 框架 | Gin |
| ORM | GORM |
| 数据库 | MySQL 8.0 |
| 缓存/消息队列 | Redis 7.0 (Streams) |
| 依赖注入 | Google Wire |
| AI / LLM | LangChainGo |
| 向量数据库 | Milvus (可降级 InMemory) |

## 核心特性

- **DDD 四层架构**：`interfaces -> application -> domain -> infrastructure`
- **支付防资损机制**：分布式锁 + 状态机约束 + 乐观锁更新
- **异步消息可靠性**：Streams 消费组 + ACK + Pending 重试处理
- **Copilot 数据助手**：`/api/v1/copilot/query` 自然语言问答
- **Copilot 风控旁路**：`risk_event_stream` 异步消费、生成 `RiskReport`、持久化落库
- **Milvus 双通道能力**：向量检索 + 风险样本回写（可配置开关）

## 目录结构

```text
aegis-pay/
├── cmd/server/                         # 启动入口 + Wire 注入
├── internal/
│   ├── interfaces/
│   │   ├── api/
│   │   │   ├── order_handler.go
│   │   │   └── copilot_api.go
│   │   └── webhooks/
│   ├── application/
│   │   ├── pay_app.go
│   │   ├── notify_app.go
│   │   └── copilot_app.go
│   ├── domain/
│   │   ├── transaction/
│   │   ├── channel/
│   │   └── copilot/
│   └── infrastructure/
│       ├── persistence/
│       ├── mq/
│       ├── channel_adapter/
│       └── ai_adapter/
├── config.yaml
├── .env.example
└── Makefile
```

## API 概览

### 支付相关

```bash
POST /api/v1/orders
GET  /api/v1/orders?trade_no=...
POST /webhooks/mock_pay_success
POST /webhooks/wechat
POST /webhooks/alipay
GET  /health
```

### Copilot 相关

```bash
POST /api/v1/copilot/query
```

请求示例：

```json
{
  "merchant_id": "M001",
  "question": "上周订单成功率是多少？"
}
```

## 配置说明

支持两种配置来源，优先级：`.env` > `config.yaml` > 代码默认值。

### 1) YAML 配置

`config.yaml` 已包含：
- `database`
- `redis`
- `app`
- `wechat`
- `alipay`
- `milvus`
- `log`

其中 `milvus` 关键项：

```yaml
milvus:
  enabled: false
  write_enabled: false
  address: "localhost:19530"
  database: ""
  collection: "risk_knowledge"
  partition: ""
  vector_field: "embedding"
  output_field: "case_text"
  metric_type: "COSINE"
  filter_expr: ""
  dimension: 64
  top_k: 3
  timeout_seconds: 3
```

### 2) 环境变量

`.env.example` 已包含全部 Milvus 变量：

- `MILVUS_ENABLED`
- `MILVUS_WRITE_ENABLED`
- `MILVUS_ADDRESS`
- `MILVUS_USERNAME`
- `MILVUS_PASSWORD`
- `MILVUS_TOKEN`
- `MILVUS_DATABASE`
- `MILVUS_COLLECTION`
- `MILVUS_PARTITION`
- `MILVUS_VECTOR_FIELD`
- `MILVUS_OUTPUT_FIELD`
- `MILVUS_METRIC_TYPE`
- `MILVUS_FILTER_EXPR`
- `MILVUS_DIMENSION`
- `MILVUS_TOP_K`
- `MILVUS_TIMEOUT_SECONDS`

## 快速开始

### 1. 启动基础设施

```bash
docker-compose up -d
```

### 2. 生成 Wire 代码

```bash
go run github.com/google/wire/cmd/wire@latest gen ./cmd/server
```

### 3. 编译运行

```bash
go mod tidy
go build -o aegis-pay.exe ./cmd/server
./aegis-pay.exe
```

### 4. 验证

```bash
go test ./...
go vet ./...
```

## 状态机

```text
INIT -> PROCESSING -> SUCCESS
  |         |
  |         v
  +------> FAILED
           |
           v
         CLOSED
```

## 开发约束

- Domain 层禁止依赖 GORM、Redis、LLM SDK
- 金额统一 `int64`（分）
- 核心状态流转必须通过状态机约束
- 外部系统访问必须通过 ACL 接口隔离

## License

MIT
