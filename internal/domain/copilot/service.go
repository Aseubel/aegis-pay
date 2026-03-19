package copilot

import (
	"errors"
	"strings"
	"time"
)

type Service struct{}

func NewService() *Service {
	return &Service{}
}

func (s *Service) ValidateQuestion(question string) error {
	trimmed := strings.TrimSpace(question)
	if trimmed == "" {
		return errors.New("question is required")
	}
	if len([]rune(trimmed)) > 500 {
		return errors.New("question is too long")
	}
	return nil
}

func (s *Service) NewQueryIntent(merchantID, question string) *QueryIntent {
	return &QueryIntent{
		IntentID:         buildID("qi"),
		MerchantID:       merchantID,
		OriginalQuestion: question,
		CreatedAt:        time.Now(),
	}
}

func (s *Service) NormalizeRiskReport(report *RiskReport) *RiskReport {
	if report == nil {
		return nil
	}
	if report.ReportID == "" {
		report.ReportID = buildID("rr")
	}
	if report.RiskScore < 0 {
		report.RiskScore = 0
	}
	if report.RiskScore > 100 {
		report.RiskScore = 100
	}
	if report.CreatedAt.IsZero() {
		report.CreatedAt = time.Now()
	}
	return report
}

func buildID(prefix string) string {
	return prefix + "-" + strings.ReplaceAll(time.Now().Format("20060102150405.000000000"), ".", "")
}
