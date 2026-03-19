package copilot

import "time"

type QueryIntent struct {
	IntentID         string
	MerchantID       string
	OriginalQuestion string
	SafeSQL          string
	AnalysisResult   string
	ExecutionMillis  int64
	CreatedAt        time.Time
}

type RiskReport struct {
	ReportID         string
	TradeNo          string
	RiskScore        int
	RiskTags         []string
	DiagnosisSummary string
	SuggestedAction  string
	CreatedAt        time.Time
}
