package ai_adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"aegis-pay/internal/domain/copilot"

	"github.com/tmc/langchaingo/llms"
)

type LangChainRAGGateway struct {
	llm         llms.Model
	vectorStore VectorStore
}

func NewLangChainRAGGateway(nl2sqlGateway *LangChainNL2SQLGateway, vectorStore VectorStore) *LangChainRAGGateway {
	return &LangChainRAGGateway{
		llm:         nl2sqlGateway.llm,
		vectorStore: vectorStore,
	}
}

func (g *LangChainRAGGateway) Analyze(ctx context.Context, transactionCtx map[string]interface{}) (*copilot.RiskReport, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	queryText := buildRiskQuery(transactionCtx)
	cases, err := g.vectorStore.Search(timeoutCtx, queryText, 0)
	if err != nil {
		return nil, err
	}

	if g.llm == nil {
		return g.heuristicRiskReport(transactionCtx, cases), nil
	}

	prompt := fmt.Sprintf(
		"你是支付反欺诈风控专家。根据交易上下文和历史欺诈案例，输出JSON对象，字段必须包含 risk_score(0-100), risk_tags([]string), diagnosis_summary, suggested_action。\n交易上下文: %v\n历史案例: %s",
		transactionCtx,
		strings.Join(cases, " | "),
	)
	raw, err := llms.GenerateFromSinglePrompt(timeoutCtx, g.llm, prompt)
	if err != nil {
		return g.heuristicRiskReport(transactionCtx, cases), nil
	}

	report := parseRiskReport(raw)
	if report == nil {
		report = g.heuristicRiskReport(transactionCtx, cases)
	}
	if tradeNo, ok := transactionCtx["trade_no"].(string); ok {
		report.TradeNo = tradeNo
	}
	_ = g.vectorStore.UpsertCase(timeoutCtx, buildRiskCaseText(transactionCtx, report), transactionCtx)
	return report, nil
}

func (g *LangChainRAGGateway) heuristicRiskReport(transactionCtx map[string]interface{}, cases []string) *copilot.RiskReport {
	score := 40
	if amount, ok := toInt64(transactionCtx["amount"]); ok {
		switch {
		case amount >= 3000000:
			score += 45
		case amount >= 1000000:
			score += 30
		case amount >= 500000:
			score += 20
		}
	}
	labels := []string{"AI_ASYNC_REVIEW"}
	if score >= 80 {
		labels = append(labels, "HIGH_AMOUNT")
	}
	if channel, ok := transactionCtx["channel_code"].(string); ok && channel != "" {
		labels = append(labels, "CHANNEL_"+strings.ToUpper(channel))
	}
	if score > 100 {
		score = 100
	}
	tradeNo, _ := transactionCtx["trade_no"].(string)
	return &copilot.RiskReport{
		ReportID:         "rr-" + strings.ReplaceAll(time.Now().Format("20060102150405.000000000"), ".", ""),
		TradeNo:          tradeNo,
		RiskScore:        score,
		RiskTags:         labels,
		DiagnosisSummary: fmt.Sprintf("命中历史样本%d条，交易进入异步复核队列。", len(cases)),
		SuggestedAction:  suggestActionByScore(score),
		CreatedAt:        time.Now(),
	}
}

func parseRiskReport(raw string) *copilot.RiskReport {
	type output struct {
		RiskScore        int      `json:"risk_score"`
		RiskTags         []string `json:"risk_tags"`
		DiagnosisSummary string   `json:"diagnosis_summary"`
		SuggestedAction  string   `json:"suggested_action"`
	}
	jsonText := extractJSONObject(raw)
	if jsonText == "" {
		return nil
	}
	var out output
	if err := json.Unmarshal([]byte(jsonText), &out); err != nil {
		return nil
	}
	return &copilot.RiskReport{
		ReportID:         "rr-" + strings.ReplaceAll(time.Now().Format("20060102150405.000000000"), ".", ""),
		RiskScore:        out.RiskScore,
		RiskTags:         out.RiskTags,
		DiagnosisSummary: out.DiagnosisSummary,
		SuggestedAction:  out.SuggestedAction,
		CreatedAt:        time.Now(),
	}
}

func buildRiskQuery(ctx map[string]interface{}) string {
	tradeNo, _ := ctx["trade_no"].(string)
	channel, _ := ctx["channel_code"].(string)
	merchantID, _ := ctx["merchant_id"].(string)
	amount, _ := toInt64(ctx["amount"])
	return fmt.Sprintf("trade_no=%s merchant_id=%s channel=%s amount=%d", tradeNo, merchantID, channel, amount)
}

func buildRiskCaseText(ctx map[string]interface{}, report *copilot.RiskReport) string {
	tradeNo, _ := ctx["trade_no"].(string)
	merchantID, _ := ctx["merchant_id"].(string)
	channel, _ := ctx["channel_code"].(string)
	amount, _ := toInt64(ctx["amount"])
	return fmt.Sprintf(
		"trade_no=%s merchant_id=%s channel=%s amount=%d score=%d tags=%s summary=%s action=%s",
		tradeNo,
		merchantID,
		channel,
		amount,
		report.RiskScore,
		strings.Join(report.RiskTags, ","),
		report.DiagnosisSummary,
		report.SuggestedAction,
	)
}

func suggestActionByScore(score int) string {
	switch {
	case score >= 85:
		return "T+1冻结清算资金并触发人工复核"
	case score >= 70:
		return "加入高危观察名单并限制大额提现"
	default:
		return "保留放行并持续监控"
	}
}

func toInt64(v interface{}) (int64, bool) {
	switch t := v.(type) {
	case int64:
		return t, true
	case int:
		return int64(t), true
	case float64:
		return int64(t), true
	case json.Number:
		num, err := t.Int64()
		return num, err == nil
	default:
		return 0, false
	}
}

func extractJSONObject(raw string) string {
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start == -1 || end == -1 || end <= start {
		return ""
	}
	return raw[start : end+1]
}
