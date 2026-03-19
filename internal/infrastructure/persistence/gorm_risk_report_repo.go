package persistence

import (
	"context"
	"encoding/json"
	"errors"

	"aegis-pay/internal/domain/copilot"

	"gorm.io/gorm"
)

type GORMRiskReportRepository struct {
	db *gorm.DB
}

func NewGORMRiskReportRepository(db *gorm.DB) *GORMRiskReportRepository {
	return &GORMRiskReportRepository{db: db}
}

func (r *GORMRiskReportRepository) Save(ctx context.Context, report *copilot.RiskReport) error {
	tags, err := json.Marshal(report.RiskTags)
	if err != nil {
		return err
	}
	po := &RiskReportPO{
		ReportID:         report.ReportID,
		TradeNo:          report.TradeNo,
		RiskScore:        report.RiskScore,
		RiskTags:         string(tags),
		DiagnosisSummary: report.DiagnosisSummary,
		SuggestedAction:  report.SuggestedAction,
		CreatedAt:        report.CreatedAt,
	}
	return r.db.WithContext(ctx).Create(po).Error
}

func (r *GORMRiskReportRepository) FindLatestByTradeNo(ctx context.Context, tradeNo string) (*copilot.RiskReport, error) {
	var po RiskReportPO
	err := r.db.WithContext(ctx).
		Where("trade_no = ?", tradeNo).
		Order("created_at desc").
		First(&po).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	var tags []string
	_ = json.Unmarshal([]byte(po.RiskTags), &tags)
	return &copilot.RiskReport{
		ReportID:         po.ReportID,
		TradeNo:          po.TradeNo,
		RiskScore:        po.RiskScore,
		RiskTags:         tags,
		DiagnosisSummary: po.DiagnosisSummary,
		SuggestedAction:  po.SuggestedAction,
		CreatedAt:        po.CreatedAt,
	}, nil
}
