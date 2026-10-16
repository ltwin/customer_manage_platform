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
	// Disabled 汇总未启用供应商或模型的原因；部分能力启用时也可能非空。
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
	aiKey, _ := credential("AICODEWITH_API_KEY")
	rhKey, _ := credential("RUNNINGHUB_API_KEY")
	if aiKey == "" && rhKey == "" {
		return Composed{Product: empty, Disabled: "未配置 AICODEWITH_API_KEY 或 RUNNINGHUB_API_KEY"}, nil
	}
	encoded := os.Getenv("CREATIVE_GENERATION_PROTECTION_KEY")
	if encoded == "" {
		return Composed{Product: empty, Disabled: "未配置 CREATIVE_GENERATION_PROTECTION_KEY"}, nil
	}
	protection, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(protection) != 32 {
		return Composed{}, fmt.Errorf("CREATIVE_GENERATION_PROTECTION_KEY must be 32 bytes in base64")
	}
	cfg := Config{Gateway: gateway, Media: media, ProtectionKey: protection, Fetchers: map[string]creativemedia.GenerationFetcher{}, References: func(scope store.AccountScope) gw.GenerationReferenceOpener {
		return media.GenerationReferenceOpenerFor(scope)
	}}
	var policies []gw.ImagePolicy
	if aiKey != "" {
		list, err := gw.AICodeWithImagePolicies(currency)
		if err != nil {
			return Composed{}, err
		}
		adapter, err := gw.NewAICodeWithImages(gw.NewHTTPClient(aicodewithHTTPTimeout), aicodewithBaseURL, aiKey)
		if err != nil {
			return Composed{}, err
		}
		fetcher, err := creativemedia.NewGenerationHTTPFetcher([]string{aicodewithFilesHost})
		if err != nil {
			return Composed{}, err
		}
		for _, p := range list {
			cfg.Adapters = append(cfg.Adapters, gw.GenerationAdapterRegistration{DeploymentID: p.Definition.DeploymentID, Version: p.Definition.AdapterVersion, Adapter: adapter, Available: func() bool { k, ok := credential("AICODEWITH_API_KEY"); return ok && k != "" }})
			cfg.Fetchers[p.Definition.DeploymentID] = fetcher
		}
		policies = append(policies, list...)
	}
	if rhKey != "" {
		list, err := gw.RunningHubImagePolicies(currency)
		if err != nil {
			return Composed{}, err
		}
		adapter, err := gw.NewRunningHubImages(gw.NewHTTPClient(aicodewithHTTPTimeout), "https://www.runninghub.ai", func() (string, bool) { return credential("RUNNINGHUB_API_KEY") })
		if err != nil {
			return Composed{}, err
		}
		fetcher, err := creativemedia.NewGenerationHTTPFetcher(gw.RunningHubImageHosts())
		if err != nil {
			return Composed{}, err
		}
		for _, model := range gw.RunningHubImageModels() {
			deployment := "runninghub-images-" + model
			cfg.Adapters = append(cfg.Adapters, gw.GenerationAdapterRegistration{DeploymentID: deployment, Version: gw.RunningHubImageAdapterVersion, Adapter: adapter, Available: func() bool {
				k, ok := credential("RUNNINGHUB_API_KEY")
				return ok && k != ""
			}})
			cfg.Fetchers[deployment] = fetcher
		}
		policies = append(policies, list...)
	}
	svc, err := New(cfg)
	if err != nil {
		return Composed{}, err
	}
	product, err := NewProduct(svc, ImageRegistrations(svc, policies))
	if err != nil {
		return Composed{}, err
	}
	return Composed{Service: svc, Product: product, Disabled: ""}, nil
}
