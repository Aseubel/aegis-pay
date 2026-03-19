# AegisPay (神盾支付) AI 智能子系统设计与开发规范

## 文档概述

本文档为 AegisPay 支付系统\*\*二期工程（AI 智能子系统）\*\*的标准架构设计与开发规范。

在核心支付链路（Transaction, Channel, Merchant 领域）已实现的基础上，本期工程将新增 **Copilot（智能助手）** 领域。利用大语言模型（LLM）、RAG（检索增强生成）与 `langchain-go` 技术，为系统提供商户级数据分析洞察与异步智能风控能力。

**核心准则：严格遵守 DDD 防腐层（ACL）规范，LangChain、向量数据库等具体 AI 基础设施实现绝对禁止侵入 Domain 领域层。**

## 一、 新增核心领域模型 (Copilot Domain)

在 `internal/domain/copilot/` 下构建新的聚合根与实体：

1. **`QueryIntent`** **(数据查询意图)**
   - **职责**：抽象商户的自然语言提问（如：“上个月微信渠道退款率是多少？”）。
   - **属性**：意图ID、商户ID、原始问题、解析后的安全SQL（可选）、分析结果。
2. **`RiskReport`** **(风控诊断报告)**
   - **职责**：记录由大模型基于交易上下文和 RAG 检索生成的风险评估结果。
   - **属性**：报告ID、关联TradeNo、风险评分(0-100)、风险标签集合、大模型诊断摘要。

## 二、 核心功能架构设计

### 功能一：商户智能数据助手 (Merchant Data Copilot - NL2SQL)

为商户控制台提供基于自然语言的复杂报表查询能力。

- **安全红线原则**：
  1. **数据隔离**：AI 生成的 SQL 只能在预先配置好的**只读从库 (Read-Replica)** 上执行。
  2. **权限隔离**：在 Prompt 构建时，必须强制注入当前请求的 `MerchantID`，利用大模型指令和后置的 AST（抽象语法树）校验，确保商户只能查询本人的订单数据（强制带有 `WHERE merchant_id = ?`）。
  3. **指令约束**：限制大模型只允许生成 `SELECT` 语句。
- **执行链路**：

  商户提问 -> Interface层接收 -> Domain层构造 `QueryIntent` -> Infrastructure层(`langchain_go`)调用 LLM 生成 SQL -> 拦截器校验 SQL 安全性 -> 执行只读从库查询 -> 将结构化数据再次输入 LLM 生成人类可读的分析结论 -> 返回商户。

### 功能二：基于 RAG 的智能风控辅助 (Smart Risk Control)

支付核心链路对延迟极其敏感（<50ms），而 LLM 调用动辄 2-5 秒。因此，AI 风控必须是**完全异步的旁路检测**。

- **技术栈**：`Milvus` / `Chroma` 向量数据库 + `Redis Streams` + `langchain-go`。
- **执行链路**：
  1. 支付核心应用层 (`Application`) 产生一笔大额/高危订单时，发布 `HighAmountTransactionEvent` 到 Redis Streams。
  2. 独立的 AI Consumer Goroutine 拉取该事件。
  3. 提取订单特征（IP、时间、设备指纹），Infrastructure 层调用向量数据库（RAG），检索相似的历史黑产欺诈案例。
  4. 将“当前订单特征”与“检索到的相似黑产案例”作为 Context 组装 Prompt，请求 LLM。
  5. LLM 输出风险评分及诊断建议，生成 `RiskReport` 实体落地入库。
  6. 若评分极高，系统可触发警报或在商户后台进行拦截标记。

## 三、 代码目录结构变更 (Delta)

在原有的 AegisPay 架构中，新增以下目录与文件：

```
aegis-pay/
├── internal/
│   ├── interfaces/
│   │   ├── api/
│   │   │   └── copilot_api.go          # 面向商户的 AI 助手 HTTP/WebSocket 接口
│   ├── application/
│   │   └── copilot_app.go              # AI 助手提问用例、异步风控消费用例编排
│   ├── domain/
│   │   └── copilot/                    # 新增 Copilot 子域 (纯净)
│   │       ├── entity.go               # RiskReport, QueryIntent 实体定义
│   │       ├── llm_gateway.go          # 核心防腐层接口 (DataAssistantGateway, RiskAnalyzerGateway)
│   │       └── service.go              # 意图校验与风控规则编排
│   └── infrastructure/
│       └── ai_adapter/                 # AI 基础设施实现 (脏活累活)
│           ├── langchain_nl2sql.go     # 基于 langchain-go 实现 NL2SQL (实现 DataAssistantGateway)
│           ├── langchain_rag.go        # 基于 langchain-go 实现文档加载与检索 (实现 RiskAnalyzerGateway)
│           └── vector_store.go         # 封装 Milvus/Chroma 向量数据库客户端


```

## 四、 防腐层 (ACL) 接口规范

Domain 层 (`internal/domain/copilot/llm_gateway.go`) 的接口设计必须保持抽象，**绝对禁止**出现 `langchain`, `llms`, `milvus` 等第三方包名：

```
package copilot

import "context"

// DataAssistantGateway 屏蔽了底层是使用 LangChain 还是手写 Prompt 的细节
type DataAssistantGateway interface {
    // AskData 接收商户自然语言，返回分析结果
    AskData(ctx context.Context, merchantID string, question string) (answer string, err error)
}

// RiskAnalyzerGateway 屏蔽了底层向量数据库和 RAG 的实现细节
type RiskAnalyzerGateway interface {
    // Analyze 接收异常交易上下文，返回风控报告
    Analyze(ctx context.Context, transactionCtx map[string]interface{}) (*RiskReport, error)
}


```

## 五、 AI 功能开发 SOP (代码生成指令指南)

请按照以下顺序指示 AI 生成代码：

1. **Step 1 - 完善领域模型 (Domain Entity & Gateway)**
   - **指令**：“请在 `internal/domain/copilot/` 目录下，编写 `entity.go` 定义 `QueryIntent` 和 `RiskReport`。然后编写 `llm_gateway.go`，按照设计文档定义 `DataAssistantGateway` 和 `RiskAnalyzerGateway` 接口。注意保持 Domain 层纯洁，不要引入任何外部 AI 框架。”
2. **Step 2 - 基础设施实现：NL2SQL 助手 (Infrastructure - NL2SQL)**
   - **指令**：“请使用 `github.com/tmc/langchaingo` 在 `infrastructure/ai_adapter/langchain_nl2sql.go` 中实现 `DataAssistantGateway` 接口。要求：构建一个包含只读数据库 Schema 的 Prompt Template；将生成的 SQL 限制为 SELECT 并强制注入 merchant\_id 参数；调用 LLM 执行后返回分析结果。”
3. **Step 3 - 基础设施实现：RAG 智能风控 (Infrastructure - RAG)**
   - **指令**：“在 `infrastructure/ai_adapter/langchain_rag.go` 中实现 `RiskAnalyzerGateway`。要求：使用 `langchaingo` 的 VectorStore 接口连接本地/内存向量库，根据传入的交易上下文检索最相似的 3 个历史欺诈案例，组合进 Prompt 后调用大模型，解析 JSON 返回 `RiskReport` 实体。”
4. **Step 4 - 异步风控应用层编排 (Application MQ Consumer)**
   - **指令**：“在 `application/copilot_app.go` 中，编写一个基于 Redis Streams 的消费者服务。功能：从 `risk_event_stream` 中拉取交易事件消息，解析后调用 Domain 层的 `RiskAnalyzerGateway` 进行风控诊断，最后将报告通过 Repository 持久化。确保消费者支持手动 ACK 并能处理熔断异常。”

