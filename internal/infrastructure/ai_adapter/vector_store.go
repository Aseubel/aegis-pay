package ai_adapter

import (
	"context"
	"fmt"
	"log"
	"math"
	"sort"
	"strings"
	"time"

	"aegis-pay/internal/config"

	milvusclient "github.com/milvus-io/milvus-sdk-go/v2/client"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"
)

type VectorStore interface {
	Search(ctx context.Context, query string, topK int) ([]string, error)
	UpsertCase(ctx context.Context, caseText string, metadata map[string]interface{}) error
}

type InMemoryVectorStore struct {
	cases []string
}

type MilvusVectorStore struct {
	client         milvusclient.Client
	collectionName string
	vectorField    string
	outputField    string
	partitionName  string
	metricType     entity.MetricType
	filterExpr     string
	dimension      int
	writeEnabled   bool
	topK           int
	timeout        time.Duration
	fallback       *InMemoryVectorStore
}

func NewVectorStore(cfg config.MilvusConfig) (VectorStore, error) {
	fallback := NewInMemoryVectorStore()
	if !cfg.Enabled {
		return fallback, nil
	}
	address := strings.TrimSpace(cfg.Address)
	if address == "" {
		return fallback, nil
	}
	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	client, err := newMilvusClient(ctx, cfg)
	if err != nil {
		log.Printf("Milvus connect failed, fallback to in-memory vector store: %v", err)
		return fallback, nil
	}

	topK := cfg.TopK
	if topK <= 0 {
		topK = 3
	}
	store := &MilvusVectorStore{
		client:         client,
		collectionName: cfg.Collection,
		vectorField:    cfg.VectorField,
		outputField:    cfg.OutputField,
		partitionName:  cfg.Partition,
		metricType:     parseMetricType(cfg.MetricType),
		filterExpr:     cfg.FilterExpr,
		dimension:      cfg.Dimension,
		writeEnabled:   cfg.WriteEnabled,
		topK:           topK,
		timeout:        timeout,
		fallback:       fallback,
	}
	if store.vectorField == "" {
		store.vectorField = "embedding"
	}
	if store.dimension <= 0 {
		store.dimension = 64
	}

	if cfg.Collection != "" {
		has, err := client.HasCollection(ctx, cfg.Collection)
		if err != nil {
			log.Printf("Milvus collection check failed, fallback to in-memory vector store: %v", err)
			_ = client.Close()
			return fallback, nil
		}
		if !has {
			log.Printf("Milvus collection %s not found, fallback to in-memory vector store", cfg.Collection)
			_ = client.Close()
			return fallback, nil
		}
	}

	return store, nil
}

func NewInMemoryVectorStore() *InMemoryVectorStore {
	return &InMemoryVectorStore{
		cases: []string{
			"同设备短时间多卡尝试且失败率高，典型盗刷试探行为",
			"新注册商户夜间大额交易密集，疑似洗钱拆单",
			"异地IP与历史常用设备不一致，且支付金额突增",
			"同一收款账户被多个高风险商户复用，存在团伙关联",
			"交易金额低频突发并伴随异常退款，疑似套利回流",
		},
	}
}

func (s *MilvusVectorStore) Search(ctx context.Context, query string, topK int) ([]string, error) {
	if topK <= 0 {
		topK = s.topK
	}
	if topK <= 0 {
		topK = 3
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	if s.collectionName != "" {
		has, err := s.client.HasCollection(timeoutCtx, s.collectionName)
		if err != nil || !has {
			return s.fallback.Search(ctx, query, topK)
		}
	}

	searchParam, err := entity.NewIndexFlatSearchParam()
	if err != nil {
		log.Printf("Milvus search param build failed, fallback to in-memory vector store: %v", err)
		return s.fallback.Search(ctx, query, topK)
	}
	queryVector := entity.FloatVector(textToFloatVector(query, s.dimension))
	outputFields := []string{}
	if s.outputField != "" {
		outputFields = append(outputFields, s.outputField)
	}

	results, err := s.client.Search(
		timeoutCtx,
		s.collectionName,
		nil,
		s.filterExpr,
		outputFields,
		[]entity.Vector{queryVector},
		s.vectorField,
		s.metricType,
		topK,
		searchParam,
	)
	if err != nil {
		log.Printf("Milvus search failed, fallback to in-memory vector store: %v", err)
		return s.fallback.Search(ctx, query, topK)
	}

	matched := extractMatchedTexts(results, s.outputField, topK)
	if len(matched) == 0 {
		return s.fallback.Search(ctx, query, topK)
	}
	return matched, nil
}

func (s *MilvusVectorStore) UpsertCase(ctx context.Context, caseText string, _ map[string]interface{}) error {
	if !s.writeEnabled {
		return s.fallback.UpsertCase(ctx, caseText, nil)
	}
	trimmed := strings.TrimSpace(caseText)
	if trimmed == "" {
		return nil
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	if s.collectionName != "" {
		has, err := s.client.HasCollection(timeoutCtx, s.collectionName)
		if err != nil || !has {
			return s.fallback.UpsertCase(ctx, caseText, nil)
		}
	}

	vector := textToFloatVector(trimmed, s.dimension)
	columns := []entity.Column{
		entity.NewColumnVarChar(s.outputField, []string{trimmed}),
		entity.NewColumnFloatVector(s.vectorField, s.dimension, [][]float32{vector}),
	}
	_, err := s.client.Insert(timeoutCtx, s.collectionName, s.partitionName, columns...)
	if err != nil {
		return s.fallback.UpsertCase(ctx, caseText, nil)
	}
	return nil
}

func (s *InMemoryVectorStore) Search(_ context.Context, query string, topK int) ([]string, error) {
	if topK <= 0 {
		topK = 3
	}
	normalized := strings.ToLower(query)
	type candidate struct {
		text  string
		score int
	}
	list := make([]candidate, 0, len(s.cases))
	for _, item := range s.cases {
		score := tokenOverlap(normalized, strings.ToLower(item))
		list = append(list, candidate{text: item, score: score})
	}
	sort.SliceStable(list, func(i, j int) bool {
		return list[i].score > list[j].score
	})
	if topK > len(list) {
		topK = len(list)
	}
	result := make([]string, 0, topK)
	for i := 0; i < topK; i++ {
		result = append(result, list[i].text)
	}
	return result, nil
}

func (s *InMemoryVectorStore) UpsertCase(_ context.Context, caseText string, _ map[string]interface{}) error {
	trimmed := strings.TrimSpace(caseText)
	if trimmed == "" {
		return nil
	}
	s.cases = append([]string{trimmed}, s.cases...)
	if len(s.cases) > 1000 {
		s.cases = s.cases[:1000]
	}
	return nil
}

func tokenOverlap(a, b string) int {
	if a == "" || b == "" {
		return 0
	}
	tokens := strings.FieldsFunc(a, func(r rune) bool {
		return r == ',' || r == '，' || r == ' ' || r == ':' || r == ';'
	})
	score := 0
	for _, token := range tokens {
		token = strings.TrimSpace(token)
		if token != "" && strings.Contains(b, token) {
			score++
		}
	}
	return score
}

func newMilvusClient(ctx context.Context, cfg config.MilvusConfig) (milvusclient.Client, error) {
	clientConfig := milvusclient.Config{
		Address: cfg.Address,
	}
	if cfg.Username != "" {
		clientConfig.Username = cfg.Username
	}
	if cfg.Password != "" {
		clientConfig.Password = cfg.Password
	}
	if cfg.Token != "" {
		clientConfig.APIKey = cfg.Token
	}
	if cfg.Database != "" {
		clientConfig.DBName = cfg.Database
	}
	client, err := milvusclient.NewClient(ctx, clientConfig)
	if err != nil {
		return nil, fmt.Errorf("new milvus client failed: %w", err)
	}
	return client, nil
}

func parseMetricType(raw string) entity.MetricType {
	normalized := strings.ToUpper(strings.TrimSpace(raw))
	switch normalized {
	case "COSINE":
		return entity.COSINE
	case "IP":
		return entity.IP
	default:
		return entity.L2
	}
}

func textToFloatVector(text string, dim int) []float32 {
	if dim <= 0 {
		dim = 64
	}
	vec := make([]float32, dim)
	runes := []rune(strings.ToLower(strings.TrimSpace(text)))
	if len(runes) == 0 {
		return vec
	}
	for i, r := range runes {
		idx := (int(r) + i*31) % dim
		if idx < 0 {
			idx += dim
		}
		vec[idx] += float32((int(r)%97)+1) / 100.0
	}

	var norm float64
	for _, v := range vec {
		norm += float64(v * v)
	}
	if norm == 0 {
		return vec
	}
	inv := 1.0 / math.Sqrt(norm)
	for i := range vec {
		vec[i] = float32(float64(vec[i]) * inv)
	}
	return vec
}

func extractMatchedTexts(results []milvusclient.SearchResult, outputField string, topK int) []string {
	matched := make([]string, 0, topK)
	for _, result := range results {
		for _, col := range result.Fields {
			if outputField != "" && col.Name() != outputField {
				continue
			}
			for i := 0; i < col.Len(); i++ {
				value, err := col.GetAsString(i)
				if err != nil {
					continue
				}
				value = strings.TrimSpace(value)
				if value == "" {
					continue
				}
				matched = append(matched, value)
				if topK > 0 && len(matched) >= topK {
					return matched
				}
			}
		}
	}
	return matched
}
