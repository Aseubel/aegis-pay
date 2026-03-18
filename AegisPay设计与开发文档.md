# AegisPay (神盾支付) 系统架构与开发规范文档

## 文档概述

本文档为 **AegisPay** 支付网关系统的标准架构设计与开发规范。本项目采用 **Go 语言单体架构 (Monolithic)**，并严格遵循 **领域驱动设计 (DDD - Domain Driven Design)** 思想。

系统旨在提供一个高可用、防资损、易扩展（随时接入新支付渠道）的统一支付中台。

**核心准则：领域层（Domain）是系统的绝对核心，必须保持纯洁，严禁泄漏任何与基础设施（数据库、网络、外部 API）相关的实现细节。**

## 一、 系统架构与 DDD 分层模型

本项目虽然是单体应用，但内部严格划分为四层架构（依赖方向严格由外向内）：

1. **User Interface 层 (用户接口层 / `interfaces`)**
   - 负责接收外部请求和数据校验。
   - 包含：HTTP API 路由 (Gin/Echo)、支付渠道异步回调 (Webhook) 处理器。
2. **Application 层 (应用服务层 / `application`)**
   - 负责用例流转（Use Cases），是领域服务的编排者。
   - 负责事务控制、分布式锁的获取、发布领域事件。
   - **禁止**在此层编写核心业务规则（如计算手续费、校验支付状态）。
3. **Domain 层 (领域层 / `domain`) - 核心！**
   - 包含业务实体 (Entity)、值对象 (Value Object)、领域服务 (Domain Service) 和防腐层接口 (Repository/Gateway Interface)。
   - 负责核心支付状态机流转、风控规则、金额计算。
   - **要求**：纯 Go 代码，没有任何外部框架（如 GORM, Redis）的 Import。
4. **Infrastructure 层 (基础设施层 / `infrastructure`)**
   - 负责实现 Domain 层定义的接口。
   - 包含：MySQL 持久化 (GORM)、Redis 缓存及 **Redis Streams 消息队列**、外部支付渠道（微信/支付宝/Stripe）的实际 API 调用。

## 二、 核心领域划分 (Bounded Contexts)

AegisPay 拆分为以下三个核心子领域：

### 1. Transaction 领域 (交易核心域)

- **核心聚合根**：`PaymentOrder` (支付单)、`RefundOrder` (退款单)。
- **职责**：维护支付订单的全生命周期状态机，生成全局唯一的系统流水号 (TradeNo)，处理支付超时与关单逻辑。

### 2. Channel 领域 (渠道路由域)

- **核心实体**：`ChannelConfig` (渠道配置)、`RoutingRule` (路由规则)。
- **职责**：屏蔽底层各种第三方支付（微信、支付宝、银联）的差异。为 Transaction 领域提供统一的防腐层接口 (`PaymentGateway`)。根据费率、可用性动态路由支付请求。

### 3. Merchant 领域 (商户支撑域)

- **核心实体**：`Merchant` (商户)、`App` (应用)、`MerchantKey` (密钥)。
- **职责**：商户进件、API 签名验签 (RSA/MD5)、商户异步通知 (Notify) 队列管理。

## 三、 核心技术栈与防资损机制

- **基础框架**：`Gin` (HTTP) + `Google Wire` (依赖注入)。
- **持久化**：`MySQL 8.0` + `GORM`。必须使用 `InnoDB`，核心表金额字段必须使用 `DECIMAL`（或用 `INT64` 存储分为单位），**绝对禁止使用 Float/Double**。
- **并发控制 (Redis)**：
  - **防并发击穿**：支付回调接口必须使用 Redis 分布式锁（以 `TradeNo` 为 Key），防止微信/支付宝同一时刻疯狂重发回调导致重复入账。
- **幂等性保障 (Idempotency)**：
  - 数据库必须建立唯一索引（如：商户号 `MerchantID` + 商户订单号 `OutTradeNo`）。
  - 所有的领域更新操作必须基于**状态机乐观锁**（如：`UPDATE orders SET status='SUCCESS' WHERE trade_no=? AND status='PROCESSING'`）。
- **异步通知 (Redis Streams)**：
  - 放弃重量级的 RabbitMQ/Kafka，采用 Redis 5.0+ 原生的 **Streams** 数据结构作为消息队列。
  - 支付成功后，通过 `XADD` 指令异步向商户通知队列投递 Webhook 任务。
  - 配合 **Consumer Groups (消费组)** 和 **PEL (Pending Entries List)** 实现消息的手动确认 (ACK)。若商户服务器宕机，可通过轮询 PEL 实现延迟阶梯重试（15s, 30s, 3m...），确保通知绝对不丢失。

## 四、 代码目录结构规范

```
aegis-pay/
├── cmd/
│   └── server/
│       └── main.go                 # 启动入口、Wire 注入中心
├── internal/
│   ├── interfaces/                 # 接口层
│   │   ├── api/                    # 面向商户的 REST API (创建订单、查询)
│   │   └── webhooks/               # 面向渠道的回调 API (微信回调、支付宝回调)
│   ├── application/                # 应用层 (Use Cases)
│   │   ├── pay_app.go              # 支付统一下单用例、回调处理用例
│   │   └── notify_app.go           # 商户通知用例 (基于 Redis Streams 消费)
│   ├── domain/                     # 领域层 (纯净)
│   │   ├── transaction/            # 交易子域
│   │   │   ├── entity.go           # PaymentOrder 实体与状态机枚举
│   │   │   ├── repository.go       # OrderRepository 接口
│   │   │   └── service.go          # 包含状态流转业务逻辑的领域服务
│   │   └── channel/                # 渠道子域
│   │       ├── entity.go
│   │       └── gateway.go          # 第三方支付渠道的防腐层接口 (Pay, Query, Refund)
│   └── infrastructure/             # 基础设施层 (脏活累活)
│       ├── persistence/            # 数据库实现
│       │   ├── gorm_order_repo.go  # 实现 domain.transaction.OrderRepository
│       │   └── models.go           # GORM 专用的 PO (持久化对象)
│       ├── channel_adapter/        # 渠道适配器
│       │   ├── mock_adapter.go     # 测试用的 Mock 渠道适配器
│       │   └── wechat_adapter.go   # 调用微信真实 API，实现 domain.channel.Gateway
│       └── mq/                     # 消息队列实现
│           └── redis_stream.go     # Redis Streams 的 XADD 和 XREADGROUP 实现
```

## 五、 核心状态机设计 (Payment Order State Machine)

支付单的状态流转必须是**单向且不可逆**的。

- `INIT` (初始化)：收到商户请求，已落地数据库，尚未向第三方渠道发起。
- `PROCESSING` (处理中)：已向第三方发起支付（如获取了微信 PrepayID），等待用户扫码/密码确认。
- `SUCCESS` (成功)：收到第三方明确的成功回调。**终态**。
- `FAILED` (失败)：支付明确失败（余额不足、风控拦截）。**终态**。
- `CLOSED` (已关闭)：支付超时未支付，被主动或被动关闭。**终态**。

**状态流转铁律**：只有处于 `INIT` 或 `PROCESSING` 状态的订单，才允许变更为 `SUCCESS`。

## 六、 DDD 开发标准 SOP (代码生成规范)

支付系统的 DDD 具有高度的模板化特征。代码生成任务必须严格按照以下自内向外的顺序执行：

1. **Step 1 - 领域实体构建 (Domain Entity)**
   - **任务要求**：构建 AegisPay 的 Transaction 领域时，首先编写 `internal/domain/transaction/entity.go`。定义 `PaymentOrder` 结构体，包含金额(分)、商户单号、系统流水号。定义状态机的 Enum 类型（INIT, PROCESSING, SUCCESS 等）。提供一个 `Success()` 方法，内部校验如果当前状态不是 PROCESSING 则返回 Error。
2. **Step 2 - 防腐层接口定义 (Domain Interface)**
   - **任务要求**：在 `internal/domain/transaction/repository.go` 中定义 `OrderRepository` 接口，包含 Save 和 FindByTradeNo。在 `internal/domain/channel/gateway.go` 中定义第三方支付的抽象接口 `PaymentGateway`，包含 `CreatePay(order *transaction.PaymentOrder) (string, error)` 方法。
3. **Step 3 - 基础设施实现 (Infrastructure)**
   - **任务要求**：使用 GORM 实现 `OrderRepository` 接口；使用 Go-Redis 实现基于 Streams 的通知发布机制。更新订单状态为 SUCCESS 时，必须使用乐观锁（在 Where 条件中加上旧状态校验）。
4. **Step 4 - 应用层编排 (Application)**
   - **任务要求**：编写 `internal/application/pay_app.go`。实现统一下单用例：1. 开启事务。2. 创建 PaymentOrder 实体并存入 Repo。3. 调用 Channel Gateway 发起支付。4. 提交事务并返回支付链接/Token给接口层。

## 七、 最小化跑通指南 (MVP 启动流程)

对于个人开发者，请按照以下步骤快速跑通 AegisPay 的核心主链路（下单 -> 模拟支付 -> Redis Streams 异步通知）。

### 1. 启动基础设施

如果你本地有 Docker，只需两行命令启动 MySQL 和 Redis：

```
docker run -d --name aegis-mysql -p 3306:3306 -e MYSQL_ROOT_PASSWORD=root -e MYSQL_DATABASE=aegis_pay mysql:8.0
docker run -d --name aegis-redis -p 6379:6379 redis:7.0
```

### 2. 初始化 Go 项目并安装核心依赖

```
go mod init aegis-pay
# 安装 Web 框架、ORM 和 Redis 客户端
go get -u [github.com/gin-gonic/gin](https://github.com/gin-gonic/gin)
go get -u gorm.io/gorm
go get -u gorm.io/driver/mysql
go get -u [github.com/redis/go-redis/v9](https://github.com/redis/go-redis/v9)
# 安装 Google Wire 依赖注入工具
go get -u [github.com/google/wire/cmd/wire](https://github.com/google/wire/cmd/wire)
```

### 3. 让 AI 编写 MVP 代码

按照文档中的 **SOP（第六节）**，依次将 prompt 发送给 AI 助手。为了快速跑通，第一版你可以告诉 AI：

- *"不需要对接真实的微信支付，在 `channel_adapter` 中写一个 `MockAdapter`，直接返回一个假的支付链接即可。"*
- *"在应用层中写一个伪造的支付回调接口，收到请求后直接调用 Order 的 Success() 方法，并通过 redis 客户端向 `notify_stream` 投递一条成功消息。"*

### 4. 依赖注入与运行

当 AI 帮你把各层的代码写完后，在 `cmd/server/` 目录下创建一个 `wire.go`，让 AI 帮你写好 `wire.Build` 逻辑。

然后在项目根目录执行：

```
wire ./cmd/server
go run ./cmd/server/main.go
```

### 5. 联调测试

启动成功后，打开终端或 Postman：

1. **测试下单 API**：POST `http://localhost:8080/api/v1/orders`，观察控制台是否成功将订单以 `INIT` 状态存入 MySQL。
2. **测试模拟回调 API**：POST `http://localhost:8080/webhooks/mock_pay_success`，传入刚生成的 TradeNo，观察订单状态是否变为 `SUCCESS`。
3. **验证 Redis Streams**：打开 Redis 客户端，输入 `XRANGE notify_stream - +`，如果看到里面静静躺着一条通知商户的 Webhook 任务，**恭喜你，DDD 支付系统的主干已经彻底跑通了！**