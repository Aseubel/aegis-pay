package ai_adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"aegis-pay/internal/config"

	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/openai"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

type LangChainNL2SQLGateway struct {
	readReplica *gorm.DB
	llm         llms.Model
	schema      string
}

func NewLangChainNL2SQLGateway(cfg config.DatabaseConfig) (*LangChainNL2SQLGateway, error) {
	readReplica, err := newReadReplicaDB(cfg)
	if err != nil {
		return nil, err
	}
	model, err := newLLMModel()
	if err != nil {
		return nil, err
	}
	return &LangChainNL2SQLGateway{
		readReplica: readReplica,
		llm:         model,
		schema:      defaultReadOnlySchema(),
	}, nil
}

func (g *LangChainNL2SQLGateway) AskData(ctx context.Context, merchantID string, question string) (string, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	sqlText, err := g.generateSQL(timeoutCtx, merchantID, question)
	if err != nil {
		return "", err
	}

	sqlText = enforceReadOnly(sqlText)
	sqlText = enforceMerchantScope(sqlText, merchantID)
	rows, err := g.executeReadOnlyQuery(timeoutCtx, sqlText)
	if err != nil {
		return "", err
	}

	if g.llm == nil {
		return rows, nil
	}

	analysisPrompt := fmt.Sprintf(
		"你是支付数据分析助手。基于以下结构化查询结果给出中文结论，强调关键数字与趋势，输出简洁结论。\n数据：%s",
		rows,
	)
	answer, err := llms.GenerateFromSinglePrompt(timeoutCtx, g.llm, analysisPrompt)
	if err != nil {
		return rows, nil
	}
	return strings.TrimSpace(answer), nil
}

func (g *LangChainNL2SQLGateway) generateSQL(ctx context.Context, merchantID, question string) (string, error) {
	if g.llm == nil {
		return fmt.Sprintf(
			"SELECT trade_no, amount, status, created_at FROM payment_orders WHERE merchant_id = '%s' ORDER BY created_at DESC LIMIT 20",
			escapeSingleQuotes(merchantID),
		), nil
	}
	prompt := fmt.Sprintf(`你是一个支付报表SQL生成器。
只允许输出一条SQL，不要包含解释文字。
必须是只读SELECT语句，禁止UPDATE/DELETE/INSERT/DDL。
必须包含 merchant_id = '%s' 过滤条件。
可用只读Schema:
%s
用户问题: %s`, escapeSingleQuotes(merchantID), g.schema, question)
	sqlText, err := llms.GenerateFromSinglePrompt(ctx, g.llm, prompt)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(sqlText), nil
}

func (g *LangChainNL2SQLGateway) executeReadOnlyQuery(ctx context.Context, sqlText string) (string, error) {
	var rows []map[string]interface{}
	if err := g.readReplica.WithContext(ctx).Raw(sqlText).Scan(&rows).Error; err != nil {
		return "", err
	}
	data, err := json.Marshal(rows)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func newReadReplicaDB(cfg config.DatabaseConfig) (*gorm.DB, error) {
	host := getenvOrDefault("MYSQL_READONLY_HOST", cfg.Host)
	port := getenvIntOrDefault("MYSQL_READONLY_PORT", cfg.Port)
	user := getenvOrDefault("MYSQL_READONLY_USER", cfg.Username)
	password := getenvOrDefault("MYSQL_READONLY_PASSWORD", cfg.Password)
	name := getenvOrDefault("MYSQL_READONLY_DATABASE", cfg.Name)
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local", user, password, host, port, name)
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxIdleConns(1)
	sqlDB.SetMaxOpenConns(5)
	sqlDB.SetConnMaxLifetime(15 * time.Minute)
	return db, nil
}

func newLLMModel() (llms.Model, error) {
	token := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	if token == "" {
		return nil, nil
	}
	model := strings.TrimSpace(os.Getenv("OPENAI_MODEL"))
	if model == "" {
		model = "gpt-4o-mini"
	}
	return openai.New(
		openai.WithToken(token),
		openai.WithModel(model),
	)
}

func enforceReadOnly(sqlText string) string {
	result := strings.TrimSpace(strings.Trim(sqlText, "`"))
	result = strings.TrimSuffix(result, ";")
	lowered := strings.ToLower(result)
	if !strings.HasPrefix(lowered, "select") {
		return "SELECT 1"
	}
	if strings.Contains(lowered, "update ") || strings.Contains(lowered, "delete ") || strings.Contains(lowered, "insert ") || strings.Contains(lowered, "drop ") || strings.Contains(lowered, "alter ") {
		return "SELECT 1"
	}
	return result
}

func enforceMerchantScope(sqlText, merchantID string) string {
	lowered := strings.ToLower(sqlText)
	if strings.Contains(lowered, " merchant_id ") || strings.Contains(lowered, "merchant_id=") {
		return sqlText
	}
	return fmt.Sprintf(
		"SELECT * FROM (%s) AS scoped_data WHERE merchant_id = '%s' LIMIT 200",
		sqlText,
		escapeSingleQuotes(merchantID),
	)
}

func escapeSingleQuotes(input string) string {
	return strings.ReplaceAll(input, "'", "''")
}

func defaultReadOnlySchema() string {
	return "payment_orders(trade_no, out_trade_no, merchant_id, app_id, amount, status, channel_code, created_at, success_at); refund_orders(refund_no, trade_no, merchant_id, refund_amount, status, created_at)"
}

func getenvOrDefault(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func getenvIntOrDefault(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	var parsed int
	_, err := fmt.Sscanf(value, "%d", &parsed)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}
