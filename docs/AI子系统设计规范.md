# AegisPay (神盾支付) AI 智能子系统设计与开发规范

## 文档概述

本文档为 AegisPay 支付系统\*\*二期工程（AI 智能子系统）\*\*的标准架构设计与开发规范。

在核心支付链路已实现的基础上，本期工程将新增 **Copilot（智能助手）** 领域。利用大语言模型（LLM）、RAG（检索增强生成）与 `langchain-go` 技术，为系统提供商户级数据分析洞察与异步智能风控能力。

**核心准则：在引入 AI 提升业务上限的同时，必须守住金融系统的安全底线。通过数据库 RLS 隔离、连接池限流、异步 T+1 策略，彻底阻断大模型可能引发的数据越权、慢查询 DoS 与主链路阻塞风险。**

## 一、 新增核心领域模型 (Copilot Domain)

在 `internal/domain/copilot/` 下构建新的聚合根与实体：

1. **`QueryIntent`** **(数据查询意图)**
   - **职责**：抽象商户的自然语言提问（如：“上个月微信渠道退款率是多少？”）。
   - **属性**：意图ID、商户ID、原始问题、解析后的安全SQL、分析结果、执行耗时。
2. **`RiskReport`** **(风控诊断报告)**
   - **职责**：记录由大模型基于交易上下文和 RAG 检索生成的风险评估结果。
   - **属性**：报告ID、关联TradeNo、风险评分(0-100)、风险标签集合、大模型诊断摘要、建议处置动作（如：次日冻结）。

## 二、 核心功能架构设计 (融合防御策略)

### 功能一：商户智能数据助手 (Merchant Data Copilot - NL2SQL)

为商户控制台提供基于自然语言的复杂报表查询能力。

- **三维安全物理防火墙（核心防御）**：
  1. **数据引擎物理隔离**：AI 生成的 SQL **仅允许**路由至专用的“只读从库 (Read-Replica)”。
  2. **数据库级 RLS (防越权)**：绝不完全依赖代码层的 AST（语法树）校验。针对 AI 查询，采用**行级安全性 (Row-Level Security)** 设计，或动态切换至仅具有该商户权限的临时 DB User。彻底杜绝大模型利用 `UNION` 或复杂子查询引发的跨租户数据泄露。
  3. **资源配额与熔断 (防 DoS)**：为 AI 服务分配**完全独立的极小数据库连接池**，避免耗尽交易主链路资源；对所有大模型生成的查询强制执行 `context.WithTimeout`（如 3 秒），超时立即触发 DB 层级的 `KILL <processlist_id>`，防止笛卡尔积慢查询打挂从库。
- **执行链路**：

  商户提问 -> Interface层接收 -> Domain层构造 `QueryIntent` -> Infrastructure层(`langchain_go`)调用 LLM 生成 SQL -> RLS沙盒环境执行查询 -> 将结构化数据输入 LLM 生成人类可读结论 -> 返回商户。

### 功能二：基于 RAG 的智能风控辅助 (Smart Risk Control)

支付核心链路对延迟极其敏感（<50ms），而 LLM 调用动辄 2-5 秒。因此，AI 风控必须采取\*\*“旁路异步 + 先放后杀”\*\*的策略。

- **技术栈**：`Milvus` / `Chroma` 向量数据库 + `Redis Streams` + `langchain-go`。
- **时效性处理策略 (先放后杀)**：
  1. **主链路（毫秒级）**：使用 C++ 极速规则引擎拦截黑白名单交易。
  2. **异步旁路（秒/分钟级）**：应用层产生异常交易后，发布 `HighAmountTransactionEvent` 到 Redis Streams。独立的 AI Consumer 异步消费，检索历史黑产特征，调用大模型生成 `RiskReport`。
  3. **T+1 拦截**：如果大模型报告判定为高危欺诈，系统无需撤回已放行的交易，而是通过内部工单系统在 **T+1 资金结算前**，冻结该商户的清算资金，完美化解 AI 延迟与资金安全之间的矛盾。

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
│           ├── langchain_nl2sql.go     # 基于 langchain-go 实现 NL2SQL 与 RLS/熔断策略
│           ├── langchain_rag.go        # 基于 langchain-go 实现 RAG 检索风控分析
│           └── vector_store.go         # 封装 Milvus/Chroma 向量数据库客户端


```

## 四、 防腐层 (ACL) 接口规范

Domain 层 (`internal/domain/copilot/llm_gateway.go`) 的接口设计必须保持抽象，**绝对禁止**出现 `langchain`, `llms`, `milvus` 等第三方包名：

```
package copilot

import "context"

// DataAssistantGateway 屏蔽了底层是使用 LangChain 还是手写 Prompt 的细节
type DataAssistantGateway interface {
    // AskData 接收商户自然语言，返回分析结果（底层需处理好 RLS 权限隔离与慢查询控制）
    AskData(ctx context.Context, merchantID string, question string) (answer string, err error)
}

// RiskAnalyzerGateway 屏蔽了底层向量数据库和 RAG 的实现细节
type RiskAnalyzerGateway interface {
    // Analyze 接收异常交易上下文，返回风控报告（用于 T+1 结算拦截参考）
    Analyze(ctx context.Context, transactionCtx map[string]interface{}) (*RiskReport, error)
}


```

## 五、 AI 功能开发 SOP (代码生成指令指南)

请按照以下顺序指示 AI 生成代码：

1. **Step 1 - 完善领域模型 (Domain Entity & Gateway)**
   - **指令**：“请在 `internal/domain/copilot/` 目录下，编写 `entity.go` 定义 `QueryIntent` 和 `RiskReport`。然后编写 `llm_gateway.go`，按照设计文档定义 `DataAssistantGateway` 和 `RiskAnalyzerGateway` 接口。注意保持 Domain 层纯洁，不要引入任何外部 AI 框架。”
2. **Step 2 - 基础设施实现：NL2SQL 助手 (Infrastructure - NL2SQL)**
   - **指令**：“请使用 `github.com/tmc/langchaingo` 在 `infrastructure/ai_adapter/langchain_nl2sql.go` 中实现 `DataAssistantGateway` 接口。要求：构建一个包含只读数据库 Schema 的 Prompt Template；在执行 AI 生成的 SQL 时，必须使用独立的数据库连接池，并通过 `context.WithTimeout` 设定 3 秒超时熔断；返回最终的结构化业务分析。”
3. **Step 3 - 基础设施实现：RAG 智能风控 (Infrastructure - RAG)**
   - **指令**：“在 `infrastructure/ai_adapter/langchain_rag.go` 中实现 `RiskAnalyzerGateway`。要求：使用 `langchaingo` 连接 Milvus 向量库，根据传入的交易上下文检索最相似的历史欺诈案例。组合 Context 调用大模型，解析 JSON 返回带有处置建议的 `RiskReport` 实体。”
4. **Step 4 - 异步风控应用层编排 (Application MQ Consumer)**
   - **指令**：“在 `application/copilot_app.go` 中，编写基于 Redis Streams 的消费者服务。从 `risk_event_stream` 拉取交易事件，调用 `RiskAnalyzerGateway` 生成风控报告，并进行持久化落盘。确保业务逻辑能够从容应对大模型偶尔的断联与超时，并支持消息的防丢 ACK。”

