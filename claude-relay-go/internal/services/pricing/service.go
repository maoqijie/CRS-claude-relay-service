package pricing

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/catstream/claude-relay-go/internal/config"
	"github.com/catstream/claude-relay-go/internal/pkg/logger"
	"github.com/catstream/claude-relay-go/internal/storage/redis"
	"github.com/fsnotify/fsnotify"
	"go.uber.org/zap"
)

// ModelPricing 模型价格（从远程 JSON 加载的格式）
type ModelPricing struct {
	InputPricePerMillion         float64 `json:"inputPricePerMillion"`
	OutputPricePerMillion        float64 `json:"outputPricePerMillion"`
	CacheCreationPricePerMillion float64 `json:"cacheCreationPricePerMillion"`
	CacheReadPricePerMillion     float64 `json:"cacheReadPricePerMillion"`
}

// RemoteModelPricing 远程 JSON 文件中的模型价格格式
type RemoteModelPricing struct {
	InputCostPerToken           float64 `json:"input_cost_per_token"`
	OutputCostPerToken          float64 `json:"output_cost_per_token"`
	CacheCreationInputTokenCost float64 `json:"cache_creation_input_token_cost"`
	CacheReadInputTokenCost     float64 `json:"cache_read_input_token_cost"`
	LiteLLMProvider             string  `json:"litellm_provider,omitempty"`
}

// UsageData 使用数据
type UsageData struct {
	InputTokens         int64 `json:"inputTokens"`
	OutputTokens        int64 `json:"outputTokens"`
	CacheCreationTokens int64 `json:"cacheCreationTokens"`
	CacheReadTokens     int64 `json:"cacheReadTokens"`
}

// CostResult 成本计算结果
type CostResult struct {
	InputCost         float64 `json:"inputCost"`
	OutputCost        float64 `json:"outputCost"`
	CacheCreationCost float64 `json:"cacheCreationCost"`
	CacheReadCost     float64 `json:"cacheReadCost"`
	TotalCost         float64 `json:"totalCost"`
}

// Service 定价服务
type Service struct {
	redis   *redis.Client
	config  config.PricingConfig
	cache   map[string]*ModelPricing
	cacheMu sync.RWMutex

	// 远程更新相关
	pricingFile     string // 本地缓存的价格文件路径
	hashFile        string // 本地缓存的哈希文件路径
	lastUpdated     time.Time
	updateTicker    *time.Ticker
	hashCheckTicker *time.Ticker
	fileWatcher     *fsnotify.Watcher
	stopChan        chan struct{}
	hashSyncMu      sync.Mutex
}

// DefaultPricing 默认价格（备用，当远程下载失败时使用）
var DefaultPricing = map[string]*ModelPricing{
	// Claude 4 系列
	"claude-sonnet-4-20250514": {
		InputPricePerMillion:         3.0,
		OutputPricePerMillion:        15.0,
		CacheCreationPricePerMillion: 3.75,
		CacheReadPricePerMillion:     0.30,
	},
	"claude-opus-4-20250514": {
		InputPricePerMillion:         15.0,
		OutputPricePerMillion:        75.0,
		CacheCreationPricePerMillion: 18.75,
		CacheReadPricePerMillion:     1.50,
	},
	// Claude 3.5 系列
	"claude-3-5-sonnet-20241022": {
		InputPricePerMillion:         3.0,
		OutputPricePerMillion:        15.0,
		CacheCreationPricePerMillion: 3.75,
		CacheReadPricePerMillion:     0.30,
	},
	"claude-3-5-sonnet-latest": {
		InputPricePerMillion:         3.0,
		OutputPricePerMillion:        15.0,
		CacheCreationPricePerMillion: 3.75,
		CacheReadPricePerMillion:     0.30,
	},
	"claude-3-5-haiku-20241022": {
		InputPricePerMillion:         0.80,
		OutputPricePerMillion:        4.0,
		CacheCreationPricePerMillion: 1.0,
		CacheReadPricePerMillion:     0.08,
	},
	"claude-3-5-haiku-latest": {
		InputPricePerMillion:         0.80,
		OutputPricePerMillion:        4.0,
		CacheCreationPricePerMillion: 1.0,
		CacheReadPricePerMillion:     0.08,
	},
	// Claude 3 系列
	"claude-3-opus-20240229": {
		InputPricePerMillion:         15.0,
		OutputPricePerMillion:        75.0,
		CacheCreationPricePerMillion: 18.75,
		CacheReadPricePerMillion:     1.50,
	},
	"claude-3-sonnet-20240229": {
		InputPricePerMillion:         3.0,
		OutputPricePerMillion:        15.0,
		CacheCreationPricePerMillion: 3.75,
		CacheReadPricePerMillion:     0.30,
	},
	"claude-3-haiku-20240307": {
		InputPricePerMillion:         0.25,
		OutputPricePerMillion:        1.25,
		CacheCreationPricePerMillion: 0.30,
		CacheReadPricePerMillion:     0.03,
	},
	// Gemini 系列
	"gemini-1.5-pro": {
		InputPricePerMillion:  3.50,
		OutputPricePerMillion: 10.50,
	},
	"gemini-1.5-flash": {
		InputPricePerMillion:  0.075,
		OutputPricePerMillion: 0.30,
	},
	"gemini-2.0-flash": {
		InputPricePerMillion:  0.10,
		OutputPricePerMillion: 0.40,
	},
	// OpenAI 系列
	"gpt-4o": {
		InputPricePerMillion:     2.50,
		OutputPricePerMillion:    10.0,
		CacheReadPricePerMillion: 1.25,
	},
	"gpt-4o-mini": {
		InputPricePerMillion:     0.15,
		OutputPricePerMillion:    0.60,
		CacheReadPricePerMillion: 0.075,
	},
	"gpt-4-turbo": {
		InputPricePerMillion:  10.0,
		OutputPricePerMillion: 30.0,
	},
	"o1": {
		InputPricePerMillion:     15.0,
		OutputPricePerMillion:    60.0,
		CacheReadPricePerMillion: 7.50,
	},
	"o1-mini": {
		InputPricePerMillion:     3.0,
		OutputPricePerMillion:    12.0,
		CacheReadPricePerMillion: 1.50,
	},
}

// NewService 创建定价服务
func NewService(redisClient *redis.Client) *Service {
	cfg := config.PricingConfig{}
	if config.Cfg != nil {
		cfg = config.Cfg.Pricing
	}

	// 确保数据目录存在
	dataDir := cfg.DataDir
	if dataDir == "" {
		dataDir = "../data"
	}

	s := &Service{
		redis:       redisClient,
		config:      cfg,
		cache:       make(map[string]*ModelPricing),
		pricingFile: filepath.Join(dataDir, "model_pricing.json"),
		hashFile:    filepath.Join(dataDir, "model_pricing.sha256"),
		stopChan:    make(chan struct{}),
	}

	// 初始化默认价格
	for model, pricing := range DefaultPricing {
		s.cache[model] = pricing
	}

	return s
}

// Initialize 初始化定价服务（启动远程更新）
func (s *Service) Initialize(ctx context.Context) error {
	// 确保数据目录存在
	dataDir := filepath.Dir(s.pricingFile)
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		logger.Warn("Failed to create data directory", zap.String("dir", dataDir), zap.Error(err))
	}

	// 检查并更新价格数据
	if err := s.checkAndUpdatePricing(ctx); err != nil {
		logger.Warn("Failed to update pricing on init", zap.Error(err))
	}

	// 初次启动时执行一次哈希校验
	go s.syncWithRemoteHash(ctx)

	// 设置定时更新
	if s.config.UpdateInterval > 0 {
		s.updateTicker = time.NewTicker(s.config.UpdateInterval)
		go s.runUpdateLoop(ctx)
	}

	// 设置哈希轮询
	if s.config.HashCheckInterval > 0 {
		s.hashCheckTicker = time.NewTicker(s.config.HashCheckInterval)
		go s.runHashCheckLoop(ctx)
		logger.Info("Pricing hash check enabled",
			zap.Duration("interval", s.config.HashCheckInterval))
	}

	// 设置文件监听
	s.setupFileWatcher()

	logger.Info("Pricing service initialized",
		zap.Int("modelCount", s.GetPricingCount()),
		zap.String("jsonUrl", s.config.JSONUrl))

	return nil
}

// Stop 停止定价服务
func (s *Service) Stop() {
	close(s.stopChan)

	if s.updateTicker != nil {
		s.updateTicker.Stop()
	}
	if s.hashCheckTicker != nil {
		s.hashCheckTicker.Stop()
	}
	if s.fileWatcher != nil {
		s.fileWatcher.Close()
	}

	logger.Info("Pricing service stopped")
}

// runUpdateLoop 运行定时更新循环
func (s *Service) runUpdateLoop(ctx context.Context) {
	for {
		select {
		case <-s.stopChan:
			return
		case <-s.updateTicker.C:
			if err := s.checkAndUpdatePricing(ctx); err != nil {
				logger.Warn("Periodic pricing update failed", zap.Error(err))
			}
		}
	}
}

// runHashCheckLoop 运行哈希校验循环
func (s *Service) runHashCheckLoop(ctx context.Context) {
	for {
		select {
		case <-s.stopChan:
			return
		case <-s.hashCheckTicker.C:
			s.syncWithRemoteHash(ctx)
		}
	}
}

// checkAndUpdatePricing 检查并更新价格数据
func (s *Service) checkAndUpdatePricing(ctx context.Context) error {
	needsUpdate := s.needsUpdate()

	if needsUpdate {
		logger.Info("Updating model pricing data...")
		if err := s.downloadPricingData(ctx); err != nil {
			logger.Warn("Failed to download pricing, using fallback", zap.Error(err))
			return s.useFallbackPricing()
		}
	} else {
		// 如果不需要更新，加载现有数据
		return s.loadPricingData()
	}

	return nil
}

// needsUpdate 检查是否需要更新
func (s *Service) needsUpdate() bool {
	info, err := os.Stat(s.pricingFile)
	if os.IsNotExist(err) {
		logger.Info("Pricing file not found, will download")
		return true
	}
	if err != nil {
		return true
	}

	fileAge := time.Since(info.ModTime())
	if fileAge > s.config.UpdateInterval {
		logger.Info("Pricing file is old, will update",
			zap.Duration("age", fileAge))
		return true
	}

	return false
}

// downloadPricingData 下载价格数据
func (s *Service) downloadPricingData(ctx context.Context) error {
	if s.config.JSONUrl == "" {
		return fmt.Errorf("pricing JSON URL not configured")
	}

	// 创建 HTTP 请求
	req, err := http.NewRequestWithContext(ctx, "GET", s.config.JSONUrl, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to download pricing: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with status: %d", resp.StatusCode)
	}

	// 读取响应体
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	// 解析 JSON
	var remotePricing map[string]*RemoteModelPricing
	if err := json.Unmarshal(body, &remotePricing); err != nil {
		return fmt.Errorf("failed to parse pricing JSON: %w", err)
	}

	// 保存到文件
	if err := os.WriteFile(s.pricingFile, body, 0644); err != nil {
		logger.Warn("Failed to save pricing file", zap.Error(err))
	}

	// 计算并保存哈希
	hash := sha256.Sum256(body)
	hashStr := hex.EncodeToString(hash[:])
	if err := os.WriteFile(s.hashFile, []byte(hashStr+"\n"), 0644); err != nil {
		logger.Warn("Failed to save hash file", zap.Error(err))
	}

	// 转换并更新缓存
	s.updateCacheFromRemote(remotePricing)
	s.lastUpdated = time.Now()

	logger.Info("Downloaded pricing data",
		zap.Int("modelCount", len(remotePricing)))

	return nil
}

// updateCacheFromRemote 从远程数据更新缓存
func (s *Service) updateCacheFromRemote(remotePricing map[string]*RemoteModelPricing) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()

	for model, remote := range remotePricing {
		s.cache[model] = &ModelPricing{
			InputPricePerMillion:         remote.InputCostPerToken * 1_000_000,
			OutputPricePerMillion:        remote.OutputCostPerToken * 1_000_000,
			CacheCreationPricePerMillion: remote.CacheCreationInputTokenCost * 1_000_000,
			CacheReadPricePerMillion:     remote.CacheReadInputTokenCost * 1_000_000,
		}
	}
}

// loadPricingData 加载本地价格数据
func (s *Service) loadPricingData() error {
	data, err := os.ReadFile(s.pricingFile)
	if err != nil {
		if os.IsNotExist(err) {
			logger.Debug("No pricing file found, using defaults")
			return nil
		}
		return fmt.Errorf("failed to read pricing file: %w", err)
	}

	var remotePricing map[string]*RemoteModelPricing
	if err := json.Unmarshal(data, &remotePricing); err != nil {
		return fmt.Errorf("failed to parse pricing file: %w", err)
	}

	s.updateCacheFromRemote(remotePricing)

	info, _ := os.Stat(s.pricingFile)
	if info != nil {
		s.lastUpdated = info.ModTime()
	}

	logger.Info("Loaded pricing data from cache",
		zap.Int("modelCount", len(remotePricing)))

	return nil
}

// useFallbackPricing 使用回退价格数据
func (s *Service) useFallbackPricing() error {
	fallbackPath := s.config.FallbackFile
	if fallbackPath == "" {
		logger.Warn("No fallback pricing file configured, using defaults")
		return nil
	}

	data, err := os.ReadFile(fallbackPath)
	if err != nil {
		if os.IsNotExist(err) {
			logger.Warn("Fallback pricing file not found", zap.String("path", fallbackPath))
			return nil
		}
		return fmt.Errorf("failed to read fallback file: %w", err)
	}

	var remotePricing map[string]*RemoteModelPricing
	if err := json.Unmarshal(data, &remotePricing); err != nil {
		return fmt.Errorf("failed to parse fallback file: %w", err)
	}

	// 复制到数据目录
	if err := os.WriteFile(s.pricingFile, data, 0644); err != nil {
		logger.Warn("Failed to copy fallback to data dir", zap.Error(err))
	}

	s.updateCacheFromRemote(remotePricing)
	s.lastUpdated = time.Now()

	logger.Warn("Using fallback pricing data",
		zap.Int("modelCount", len(remotePricing)))

	return nil
}

// syncWithRemoteHash 与远端哈希对比
func (s *Service) syncWithRemoteHash(ctx context.Context) {
	if !s.hashSyncMu.TryLock() {
		return // 已有同步在进行中
	}
	defer s.hashSyncMu.Unlock()

	if s.config.HashUrl == "" {
		return
	}

	// 获取远端哈希
	remoteHash, err := s.fetchRemoteHash(ctx)
	if err != nil {
		logger.Debug("Failed to fetch remote hash", zap.Error(err))
		return
	}

	// 计算本地哈希
	localHash := s.computeLocalHash()
	if localHash == "" {
		logger.Info("Local pricing file missing, downloading...")
		s.downloadPricingData(ctx)
		return
	}

	// 比较哈希
	if remoteHash != localHash {
		logger.Info("Remote pricing file updated, downloading...",
			zap.String("localHash", localHash[:8]+"..."),
			zap.String("remoteHash", remoteHash[:8]+"..."))
		s.downloadPricingData(ctx)
	}
}

// fetchRemoteHash 获取远端哈希值
func (s *Service) fetchRemoteHash(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", s.config.HashUrl, nil)
	if err != nil {
		return "", err
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("hash fetch failed with status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	// 解析哈希（格式：hash  filename 或 hash\n）
	hash := strings.TrimSpace(string(body))
	parts := strings.Fields(hash)
	if len(parts) > 0 {
		return parts[0], nil
	}

	return "", fmt.Errorf("empty hash response")
}

// computeLocalHash 计算本地文件哈希
func (s *Service) computeLocalHash() string {
	// 先尝试读取缓存的哈希
	if data, err := os.ReadFile(s.hashFile); err == nil {
		hash := strings.TrimSpace(string(data))
		if hash != "" {
			return hash
		}
	}

	// 计算文件哈希
	data, err := os.ReadFile(s.pricingFile)
	if err != nil {
		return ""
	}

	hash := sha256.Sum256(data)
	hashStr := hex.EncodeToString(hash[:])

	// 保存哈希
	os.WriteFile(s.hashFile, []byte(hashStr+"\n"), 0644)

	return hashStr
}

// setupFileWatcher 设置文件监听
func (s *Service) setupFileWatcher() {
	if _, err := os.Stat(s.pricingFile); os.IsNotExist(err) {
		logger.Debug("Pricing file does not exist yet, skipping watcher setup")
		return
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		logger.Warn("Failed to create file watcher", zap.Error(err))
		return
	}

	s.fileWatcher = watcher

	go func() {
		debounceTimer := time.NewTimer(0)
		if !debounceTimer.Stop() {
			<-debounceTimer.C
		}

		for {
			select {
			case <-s.stopChan:
				return
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Op&(fsnotify.Write|fsnotify.Create) != 0 {
					// 防抖：500ms 内的多次变更只触发一次重载
					debounceTimer.Reset(500 * time.Millisecond)
				}
			case <-debounceTimer.C:
				logger.Info("Reloading pricing data due to file change...")
				if err := s.loadPricingData(); err != nil {
					logger.Warn("Failed to reload pricing data", zap.Error(err))
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				logger.Warn("File watcher error", zap.Error(err))
			}
		}
	}()

	if err := watcher.Add(s.pricingFile); err != nil {
		logger.Warn("Failed to watch pricing file", zap.Error(err))
	} else {
		logger.Info("File watcher set up for pricing file")
	}
}

// ForceUpdate 强制更新价格数据
func (s *Service) ForceUpdate(ctx context.Context) error {
	logger.Info("Force updating pricing data...")
	if err := s.downloadPricingData(ctx); err != nil {
		logger.Warn("Force update failed, using fallback", zap.Error(err))
		return s.useFallbackPricing()
	}
	return nil
}

// GetPricing 获取模型价格
func (s *Service) GetPricing(model string) *ModelPricing {
	s.cacheMu.RLock()
	defer s.cacheMu.RUnlock()

	// 精确匹配
	if pricing, ok := s.cache[model]; ok {
		return pricing
	}

	// 模糊匹配（处理版本后缀等变体）
	modelLower := strings.ToLower(model)
	for key, pricing := range s.cache {
		keyLower := strings.ToLower(key)
		if strings.Contains(modelLower, keyLower) || strings.Contains(keyLower, modelLower) {
			return pricing
		}
	}

	// 模型系列匹配
	pricing := s.matchByModelFamily(modelLower)
	if pricing != nil {
		return pricing
	}

	// 返回默认值（Sonnet 价格）
	return DefaultPricing["claude-3-5-sonnet-20241022"]
}

// matchByModelFamily 按模型系列匹配
func (s *Service) matchByModelFamily(modelLower string) *ModelPricing {
	// Claude 系列
	if strings.Contains(modelLower, "opus") {
		if strings.Contains(modelLower, "claude-4") || strings.Contains(modelLower, "claude-opus-4") {
			return DefaultPricing["claude-opus-4-20250514"]
		}
		return DefaultPricing["claude-3-opus-20240229"]
	}
	if strings.Contains(modelLower, "sonnet") {
		if strings.Contains(modelLower, "claude-4") || strings.Contains(modelLower, "claude-sonnet-4") {
			return DefaultPricing["claude-sonnet-4-20250514"]
		}
		return DefaultPricing["claude-3-5-sonnet-20241022"]
	}
	if strings.Contains(modelLower, "haiku") {
		return DefaultPricing["claude-3-5-haiku-20241022"]
	}

	// Gemini 系列
	if strings.Contains(modelLower, "gemini") {
		if strings.Contains(modelLower, "pro") {
			return DefaultPricing["gemini-1.5-pro"]
		}
		if strings.Contains(modelLower, "flash") {
			return DefaultPricing["gemini-2.0-flash"]
		}
	}

	// OpenAI 系列
	if strings.Contains(modelLower, "gpt-4o") {
		if strings.Contains(modelLower, "mini") {
			return DefaultPricing["gpt-4o-mini"]
		}
		return DefaultPricing["gpt-4o"]
	}
	if strings.Contains(modelLower, "o1") {
		if strings.Contains(modelLower, "mini") {
			return DefaultPricing["o1-mini"]
		}
		return DefaultPricing["o1"]
	}

	return nil
}

// CalculateCost 计算成本
func (s *Service) CalculateCost(model string, usage UsageData) *CostResult {
	pricing := s.GetPricing(model)
	if pricing == nil {
		return &CostResult{}
	}

	result := &CostResult{
		InputCost:         float64(usage.InputTokens) * pricing.InputPricePerMillion / 1_000_000,
		OutputCost:        float64(usage.OutputTokens) * pricing.OutputPricePerMillion / 1_000_000,
		CacheCreationCost: float64(usage.CacheCreationTokens) * pricing.CacheCreationPricePerMillion / 1_000_000,
		CacheReadCost:     float64(usage.CacheReadTokens) * pricing.CacheReadPricePerMillion / 1_000_000,
	}

	result.TotalCost = result.InputCost + result.OutputCost + result.CacheCreationCost + result.CacheReadCost

	return result
}

// CalculateTotalCost 计算总成本（简化版）
func (s *Service) CalculateTotalCost(model string, usage UsageData) float64 {
	result := s.CalculateCost(model, usage)
	return result.TotalCost
}

// SetPricing 设置模型价格
func (s *Service) SetPricing(model string, pricing *ModelPricing) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	s.cache[model] = pricing
}

// GetAllPricing 获取所有价格
func (s *Service) GetAllPricing() map[string]*ModelPricing {
	s.cacheMu.RLock()
	defer s.cacheMu.RUnlock()

	result := make(map[string]*ModelPricing, len(s.cache))
	for k, v := range s.cache {
		result[k] = v
	}
	return result
}

// LoadFromRedis 从 Redis 加载价格
func (s *Service) LoadFromRedis(ctx context.Context) error {
	if s.redis == nil {
		return nil
	}

	data, err := s.redis.Get(ctx, "model_pricing")
	if err != nil {
		logger.Debug("No pricing data in Redis, using defaults")
		return nil
	}
	if data == "" {
		return nil
	}

	var pricing map[string]*ModelPricing
	if err := json.Unmarshal([]byte(data), &pricing); err != nil {
		logger.Warn("Failed to unmarshal pricing from Redis", zap.Error(err))
		return err
	}

	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()

	for model, p := range pricing {
		s.cache[model] = p
	}

	logger.Info("Loaded pricing from Redis", zap.Int("count", len(pricing)))
	return nil
}

// SaveToRedis 保存价格到 Redis
func (s *Service) SaveToRedis(ctx context.Context) error {
	if s.redis == nil {
		return nil
	}

	s.cacheMu.RLock()
	data, err := json.Marshal(s.cache)
	s.cacheMu.RUnlock()

	if err != nil {
		return err
	}

	return s.redis.Set(ctx, "model_pricing", string(data), 0)
}

// UpdatePricing 批量更新价格
func (s *Service) UpdatePricing(pricing map[string]*ModelPricing) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()

	for model, p := range pricing {
		s.cache[model] = p
	}
}

// DeletePricing 删除模型价格
func (s *Service) DeletePricing(model string) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	delete(s.cache, model)
}

// ResetToDefaults 重置为默认价格
func (s *Service) ResetToDefaults() {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()

	s.cache = make(map[string]*ModelPricing)
	for model, pricing := range DefaultPricing {
		s.cache[model] = pricing
	}
}

// GetPricingCount 获取价格条目数量
func (s *Service) GetPricingCount() int {
	s.cacheMu.RLock()
	defer s.cacheMu.RUnlock()
	return len(s.cache)
}

// GetStatus 获取服务状态
func (s *Service) GetStatus() map[string]interface{} {
	s.cacheMu.RLock()
	modelCount := len(s.cache)
	s.cacheMu.RUnlock()

	return map[string]interface{}{
		"initialized":    true,
		"lastUpdated":    s.lastUpdated,
		"modelCount":     modelCount,
		"pricingUrl":     s.config.JSONUrl,
		"updateInterval": s.config.UpdateInterval.String(),
	}
}
