package channel

import "context"

type ChannelConfig struct {
	Code        string  // 渠道编码 (wechat/alipay/stripe)
	Name        string  // 渠道名称
	AppID       string  // 应用ID
	MerchantID  string  // 商户号
	APIKey      string  // API密钥
	CertPath    string  // 证书路径
	CallbackURL string  // 回调地址
	Enabled     bool    // 是否启用
	Priority    int     // 优先级
	Rate        float64 // 费率
	Available   bool    // 可用性
}

type RoutingRule struct {
	RuleID       string   // 规则ID
	Name         string   // 规则名称
	MerchantID   string   // 商户号 (空表示通用)
	AppID        string   // 应用ID (空表示通用)
	ChannelCodes []string // 匹配的渠道编码
	Condition    string   // 条件表达式
	Priority     int      // 优先级
	Enabled      bool     // 是否启用
}

type ChannelService struct {
	configs map[string]*ChannelConfig
	rules   []*RoutingRule
}

func NewChannelService() *ChannelService {
	return &ChannelService{
		configs: make(map[string]*ChannelConfig),
		rules:   make([]*RoutingRule, 0),
	}
}

func (s *ChannelService) GetChannel(code string) *ChannelConfig {
	return s.configs[code]
}

func (s *ChannelService) GetEnabledChannels() []*ChannelConfig {
	var enabled []*ChannelConfig
	for _, c := range s.configs {
		if c.Enabled && c.Available {
			enabled = append(enabled, c)
		}
	}
	return enabled
}

func (s *ChannelService) GetBestChannel(ctx context.Context, merchantID, appID string) *ChannelConfig {
	channels := s.GetEnabledChannels()
	if len(channels) == 0 {
		return nil
	}
	var best *ChannelConfig
	bestRate := 1.0
	for _, c := range channels {
		if c.Rate < bestRate {
			bestRate = c.Rate
			best = c
		}
	}
	return best
}

func (s *ChannelService) AddChannel(config *ChannelConfig) {
	s.configs[config.Code] = config
}

func (s *ChannelService) AddRule(rule *RoutingRule) {
	s.rules = append(s.rules, rule)
}
