# AegisPay 神盾支付

高可用、防资损、易扩展的统一支付中台。

## 项目简介

AegisPay 是一个基于 Go 语言开发的支付网关系统，采用**单体架构**与**领域驱动设计 (DDD)** 思想构建。系统提供统一下单、支付回调、订单查询、退款等核心支付能力，支持多支付渠道（微信、支付宝、Stripe）动态路由。

## 技术栈

| 分类 | 技术 |
|------|------|
| 语言 | Go 1.21+ |
| Web 框架 | Gin |
| ORM | GORM |
| 数据库 | MySQL 8.0 (InnoDB) |
| 缓存/消息队列 | Redis 7.0 (Streams) |
| 依赖注入 | Google Wire |

## 核心特性

- **DDD 四层架构**：Domain（领域层）保持纯净，无外部依赖
- **状态机保障**：支付订单状态流转严格遵循 INIT → PROCESSING → SUCCESS/FAILED/CLOSED
- **防资损机制**：
  - Redis 分布式锁防止并发击穿
  - 乐观锁保障状态变更原子性
  - 金额使用 int64（分）存储，避免浮点精度问题
- **异步通知**：基于 Redis Streams 的商户 Webhook 通知，支持消息持久化和重试

## 目录结构

```
aegis-pay/
├── cmd/server/
│   └── main.go                 # 应用入口
├── internal/
│   ├── domain/                 # 领域层（纯净）
│   │   ├── transaction/        # 交易子域
│   │   │   ├── entity.go      # PaymentOrder 实体
│   │   │   ├── refund_entity.go
│   │   │   ├── repository.go   # 仓储接口
│   │   │   └── service.go     # 领域服务
│   │   └── channel/           # 渠道子域
│   │       ├── entity.go      # 渠道配置
│   │       └── gateway.go     # 支付网关接口
│   ├── application/           # 应用层
│   │   ├── pay_app.go        # 支付用例
│   │   └── notify_app.go     # 通知用例
│   ├── infrastructure/        # 基础设施层
│   │   ├── persistence/      # 数据库实现
│   │   ├── channel_adapter/  # 渠道适配器
│   │   └── mq/              # Redis Streams 实现
│   └── interfaces/           # 接口层
│       ├── api/             # REST API
│       └── webhooks/        # 回调处理
├── go.mod
└── go.sum
```

## API 文档

### 创建支付订单

```bash
POST /api/v1/orders
Content-Type: application/json

{
  "merchant_id": "M202401001",
  "app_id": "APP001",
  "out_trade_no": "ORD20240119001",
  "amount": 100,
  "description": "测试订单"
}
```

**响应**

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "trade_no": "AP170565432100123456",
    "out_trade_no": "ORD20240119001",
    "amount": 100,
    "pay_url": "https://mock-pay.example.com/gateway?trade_no=...",
    "qr_code_url": "https://mock-pay.example.com/qrcode/...",
    "channel_code": "mock",
    "expired_at": 1705657223
  }
}
```

### 查询订单

```bash
GET /api/v1/orders?trade_no=AP170565432100123456
```

### 模拟支付回调

```bash
POST /webhooks/mock_pay_success
Content-Type: application/json

{
  "trade_no": "AP170565432100123456"
}
```

### 健康检查

```bash
GET /health
```

## 快速开始

### 1. 启动基础设施

**方式一：使用 docker-compose（推荐）**

```bash
docker-compose up -d
```

**方式二：手动启动容器**

```bash
docker run -d --name aegis-mysql -p 3306:3306 \
  -e MYSQL_ROOT_PASSWORD=root \
  -e MYSQL_DATABASE=aegis_pay \
  mysql:8.0

docker run -d --name aegis-redis -p 6379:6379 redis:7.0
```

### 2. 编译运行

```bash
go mod tidy
go build -o aegis-pay.exe ./cmd/server
./aegis-pay.exe
```

### 3. 测试

```bash
# 创建订单
curl -X POST http://localhost:8080/api/v1/orders \
  -H "Content-Type: application/json" \
  -d '{"merchant_id":"M001","app_id":"APP001","out_trade_no":"T001","amount":100,"description":"test"}'

# 模拟支付成功
curl -X POST http://localhost:8080/webhooks/mock_pay_success \
  -H "Content-Type: application/json" \
  -d '{"trade_no":"你获取到的trade_no"}'

# 查询 Redis Streams 通知队列
redis-cli
XRANGE notify_stream - +
```

## 支付状态机

```
  INIT ──────► PROCESSING ──────► SUCCESS
    │               │
    │               │
    ▼               ▼
 CLOSED ◄───── FAILED
```

- **INIT**：订单已创建，等待第三方渠道
- **PROCESSING**：已向渠道发起支付，等待用户确认
- **SUCCESS**：支付成功（终态）
- **FAILED**：支付失败（终态）
- **CLOSED**：订单关闭（终态）

## 项目规范

- 领域层代码禁止引入 GORM、Redis 等基础设施依赖
- 金额字段统一使用 int64，单位为分
- 数据库更新必须使用乐观锁（状态校验）
- 所有外部调用必须通过防腐层接口隔离

## License

MIT
