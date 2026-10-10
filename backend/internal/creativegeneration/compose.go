package creativegeneration

import (
	"encoding/base64"
	"fmt"
	"os"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/creativemedia"
	gw "github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const (
	aicodewithBaseURL = "https://api.aicodewith.ai"
	// 中转结果文件的取件域名（2026-10-09实测，与API域名不同）。
	aicodewithFilesHost = "api.aicodewith.com"
	// 生图提交要上传最多9张参考图，查询很快；单次HTTP给足时间，仍受运行时期限约束。
	aicodewithHTTPTimeout = 2 * time.Minute
)

// Composed 是组合根需要的生成能力；Service为nil表示生成未启用，Product是空目录。
type Composed struct {
	Service *Service
	Product *Product
	// Disabled 说明未启用的原因，供启动日志使用；启用时为空。
	Disabled string
}

// Compose 按环境装配内置生图模型。缺少凭证或保护密钥时只关闭生成，不影响对话与画布。
// API进程与worker进程必须用同一份配置调用，保证两边看到同一套部署与规则。
func Compose(gateway *gw.Service, currency string, media *creativemedia.Service, credential func(string) (string, bool)) (Composed, error) {
	empty, err := NewProduct(nil, nil)
	if err != nil {
		return Composed{}, err
	}
	// 与网关一致：未注入凭证读取函数时从环境变量读。
	if credential == nil {
		credential = os.LookupEnv
	}
	key, ok := credential("AICODEWITH_API_KEY")
	if !ok || key == "" {
		return Composed{Product: empty, Disabled: "未配置 AICODEWITH_API_KEY"}, nil
	}
	encoded := os.Getenv("CREATIVE_GENERATION_PROTECTION_KEY")
	if encoded == "" {
		return Composed{Product: empty, Disabled: "未配置 CREATIVE_GENERATION_PROTECTION_KEY"}, nil
	}
	protection, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(protection) != 32 {
		return Composed{}, fmt.Errorf("CREATIVE_GENERATION_PROTECTION_KEY must be 32 bytes in base64")
	}
	policies, err := gw.AICodeWithImagePolicies(currency)
	if err != nil {
		return Composed{}, err
	}
	adapter, err := gw.NewAICodeWithImages(gw.NewHTTPClient(aicodewithHTTPTimeout), aicodewithBaseURL, key)
	if err != nil {
		return Composed{}, err
	}
	fetcher, err := creativemedia.NewGenerationHTTPFetcher([]string{aicodewithFilesHost})
	if err != nil {
		return Composed{}, err
	}
	cfg := Config{Gateway: gateway, Media: media, ProtectionKey: protection, Fetchers: map[string]creativemedia.GenerationFetcher{},
		References: func(scope store.AccountScope) gw.GenerationReferenceOpener {
			return media.GenerationReferenceOpenerFor(scope)
		}}
	for _, p := range policies {
		// 三个模型共用一个中转账号，限流键相同。
		cfg.Adapters = append(cfg.Adapters, gw.GenerationAdapterRegistration{DeploymentID: p.Definition.DeploymentID, Version: p.Definition.AdapterVersion, LimitKey: "aicodewith/images", Concurrency: 4, RateLimit: 30, Adapter: adapter})
		cfg.Fetchers[p.Definition.DeploymentID] = fetcher
	}
	svc, err := New(cfg)
	if err != nil {
		return Composed{}, err
	}
	product, err := NewProduct(svc, ImageRegistrations(svc, policies))
	if err != nil {
		return Composed{}, err
	}
	return Composed{Service: svc, Product: product}, nil
}
