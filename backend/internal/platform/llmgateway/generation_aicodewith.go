package llmgateway

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"
	"time"
)

// AICodeWithImageAdapterVersion 绑定下面的协议映射；字段映射或状态解释变化须升版本。
const AICodeWithImageAdapterVersion = "aicodewith-images-2"

// aicodewithImageModels 是中转上可用的GPT Image模型；model_key与中转模型名一致。
var aicodewithImageModels = []string{"gpt-image-2", "gpt-image-2.5-sunburst", "gpt-image-2.5-flare", "gpt-image-2.6-sunburst", "gpt-image-2.6-flare"}

const (
	maxAICodeWithTaskBytes = 1 << 20
	// 中转结果文件无鉴权、不重定向，实测缓存3天；取件期限按1天保守声明，过期后
	// 由Observe重新取得同一地址。
	aicodewithResultTTL = 24 * time.Hour
	// 中转不返回结果文件大小；这里声明的是取件上限，实际字节由媒体校验器确定。
	aicodewithImageMaxBytes = 25 << 20
)

// AICodeWithImages 是AiCodeWith异步生图协议：创建任务后轮询，取消接口不生效。
// 实测结论见 .codestable/features/2026-10-09-image-generation/image-generation-design.md §1；
// 2026-11-09中转改版：每个任务只出一张图、按次计费不回报用量、参考图只收公网HTTPS地址。
type AICodeWithImages struct {
	client  *http.Client
	baseURL string
	key     string
	now     func() time.Time
}

func NewAICodeWithImages(client *http.Client, baseURL, credential string) (*AICodeWithImages, error) {
	u, err := url.Parse(baseURL)
	if client == nil || credential == "" || err != nil || u.Scheme != "https" || u.Host == "" {
		return nil, fmt.Errorf("%w: aicodewith image adapter", ErrValidation)
	}
	return &AICodeWithImages{client: client, baseURL: strings.TrimRight(baseURL, "/"), key: credential, now: time.Now}, nil
}

// aicodewithImageParameters 与模式schema同名；默认值已由规则补齐。
type aicodewithImageParameters struct {
	AspectRatio string `json:"aspect_ratio"`
	Resolution  string `json:"resolution"`
	Quality     string `json:"quality"`
	Background  string `json:"background"`
}

type aicodewithImageRequest struct {
	Model      string   `json:"model"`
	Prompt     string   `json:"prompt"`
	Size       string   `json:"size"`
	Resolution string   `json:"resolution,omitempty"`
	Quality    string   `json:"quality"`
	Background string   `json:"background,omitempty"`
	ImageURLs  []string `json:"image_urls,omitempty"`
}

type aicodewithTask struct {
	ID         string   `json:"id"`
	Status     string   `json:"status"`
	Progress   *float64 `json:"progress"`
	ResultData []struct {
		URL string `json:"url"`
	} `json:"result_data"`
	Error json.RawMessage `json:"error"`
}

func (a *AICodeWithImages) Submit(ctx context.Context, call GenerationCall) (GenerationEvent, error) {
	body, err := a.requestBody(ctx, call)
	if err != nil {
		// 请求体没组装成，什么都没有发出。
		return GenerationEvent{}, fmt.Errorf("%w: %w", ErrNotDispatched, err)
	}
	status, raw, err := a.do(ctx, http.MethodPost, "/v1/images/generations", body)
	if err != nil {
		return GenerationEvent{}, fmt.Errorf("aicodewith submit: %w", err)
	}
	// 这些状态在创建时同步返回、没有任务ID，中转明确没有受理。
	if code, ok := map[int]string{
		http.StatusBadRequest:         "invalid_parameter",
		http.StatusUnauthorized:       "credential_refused",
		http.StatusPaymentRequired:    "insufficient_balance",
		http.StatusServiceUnavailable: "no_available_channel",
	}[status]; ok {
		return GenerationEvent{EventKey: "submit", Observation: GenerationObservation{State: GenerationRejected, ErrorCode: code}, Diagnostic: fmt.Sprintf("http_status=%d", status)}, nil
	}
	if status != http.StatusOK {
		// 其他状态无法证明未受理；交给运行时按受理未知处理，绝不重新提交。
		return GenerationEvent{}, fmt.Errorf("%w: aicodewith submit status %d", ErrProtocol, status)
	}
	var task aicodewithTask
	if err := json.Unmarshal(raw, &task); err != nil || task.ID == "" {
		return GenerationEvent{}, fmt.Errorf("%w: aicodewith submit answer", ErrProtocol)
	}
	return GenerationEvent{EventKey: "submit", Observation: GenerationObservation{State: GenerationAccepted, TaskID: task.ID}}, nil
}

func (a *AICodeWithImages) Observe(ctx context.Context, call GenerationCall) (GenerationEvent, error) {
	if call.TaskID == "" {
		return GenerationEvent{}, fmt.Errorf("%w: aicodewith task identity", ErrValidation)
	}
	status, raw, err := a.do(ctx, http.MethodGet, "/v1/tasks/"+url.PathEscape(call.TaskID), nil)
	if err != nil {
		return GenerationEvent{}, fmt.Errorf("aicodewith query: %w", err)
	}
	if status == http.StatusNotFound {
		return GenerationEvent{EventKey: "not_found", Observation: GenerationObservation{State: GenerationUnknown, ErrorCode: "task_not_found"}}, nil
	}
	if status != http.StatusOK {
		return GenerationEvent{}, fmt.Errorf("%w: aicodewith task status %d", ErrProtocol, status)
	}
	var task aicodewithTask
	if err := json.Unmarshal(raw, &task); err != nil || task.ID != call.TaskID {
		return GenerationEvent{}, fmt.Errorf("%w: aicodewith task answer", ErrProtocol)
	}
	switch task.Status {
	case "processing", "pending", "queued":
		o := GenerationObservation{State: GenerationRunning, TaskID: task.ID}
		key := "running"
		if task.Progress != nil && *task.Progress >= 0 && *task.Progress < 100 {
			p := *task.Progress / 100
			o.Progress = &p
			key = fmt.Sprintf("running-%d", int(*task.Progress))
		}
		return GenerationEvent{EventKey: key, Observation: o}, nil
	case "completed":
		outputs, err := a.outputs(task)
		if err != nil {
			return GenerationEvent{}, err
		}
		// 中转不回报用量、按成功张数计费；张数即结果数。
		return GenerationEvent{EventKey: "completed", Observation: GenerationObservation{State: GenerationCompleted, TaskID: task.ID, Outputs: outputs}, Usage: map[string]int64{"images": int64(len(outputs))}}, nil
	case "failed", "cancelled":
		// 失败与取消的任务不扣费（中转文档）。
		code := task.errorCode()
		if task.Status == "cancelled" {
			code = "provider_cancelled"
		}
		return GenerationEvent{EventKey: task.Status, Observation: GenerationObservation{State: GenerationFailed, TaskID: task.ID, ErrorCode: code}, Usage: map[string]int64{"images": 0}, Diagnostic: "provider_error=" + task.providerErrorCode()}, nil
	default:
		return GenerationEvent{}, fmt.Errorf("%w: aicodewith task state %s", ErrProtocol, providerToken(task.Status))
	}
}

// Cancel 不调用远端：DELETE返回成功但任务照常完成并扣费（2026-10-09实测）。
// 这里只返回当前状态，不声称已取消。
func (a *AICodeWithImages) Cancel(ctx context.Context, call GenerationCall) (GenerationEvent, error) {
	return a.Observe(ctx, call)
}

func (a *AICodeWithImages) requestBody(ctx context.Context, call GenerationCall) ([]byte, error) {
	r := call.Prepared.Generation().Snapshot().Request
	if !slices.Contains(aicodewithImageModels, r.ModelKey) || r.Kind != GenerationImage {
		return nil, fmt.Errorf("%w: aicodewith image model", ErrCapability)
	}
	var p aicodewithImageParameters
	if err := json.Unmarshal(r.Input.Parameters, &p); err != nil || p.AspectRatio == "" || p.Quality == "" {
		return nil, fmt.Errorf("%w: aicodewith image parameters", ErrValidation)
	}
	// GPT Image 2与2.5线上中转会丢弃quality，照发无害；2.6按五档校验。
	body := aicodewithImageRequest{Model: r.ModelKey, Prompt: r.Input.Prompt, Size: p.AspectRatio, Quality: p.Quality}
	// 中转只在比例格式下解释清晰度；auto时不发，避免与界面置灰的含义不一致。
	if p.AspectRatio != "auto" {
		body.Resolution = p.Resolution
	}
	if p.Background != "" && p.Background != "auto" {
		body.Background = p.Background
	}
	for i, ref := range r.Input.References {
		// 文字参考已经通过句内引用编进提示词，只外发图片。
		if ref.Kind != GenerationImage {
			continue
		}
		// 中转拒收data URL，只收公网HTTPS；地址指向冻结的固定对象版本。
		linker, ok := call.References.(GenerationReferenceLinker)
		if !ok {
			return nil, fmt.Errorf("%w: generation reference links", ErrUnavailable)
		}
		location, err := linker.LinkGenerationReference(ctx, ref)
		if err != nil {
			return nil, fmt.Errorf("reference %d (%s) link: %w", i, ref.RevisionID, err)
		}
		if u, err := url.Parse(location); err != nil || u.Scheme != "https" || u.Host == "" {
			return nil, fmt.Errorf("%w: reference %d link is not https", ErrValidation, i)
		}
		body.ImageURLs = append(body.ImageURLs, location)
	}
	return json.Marshal(body)
}

// readGenerationReference 只外发与冻结摘要完全一致的字节。
func readGenerationReference(ctx context.Context, opener GenerationReferenceOpener, ref GenerationReference) ([]byte, error) {
	if opener == nil {
		return nil, fmt.Errorf("%w: generation reference reader", ErrUnavailable)
	}
	reader, err := opener.OpenGenerationReference(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	defer func() { _ = reader.Close() }()
	data, err := io.ReadAll(io.LimitReader(reader, ref.ByteSize+1))
	if err != nil {
		return nil, fmt.Errorf("read after %d bytes: %w", len(data), err)
	}
	sum := sha256.Sum256(data)
	if int64(len(data)) != ref.ByteSize || hex.EncodeToString(sum[:]) != ref.Digest {
		return nil, fmt.Errorf("%w: generation reference bytes (read %d, frozen %d, digest_match=%t)", ErrConflict, len(data), ref.ByteSize, hex.EncodeToString(sum[:]) == ref.Digest)
	}
	return data, nil
}

func (a *AICodeWithImages) do(ctx context.Context, method, route string, body []byte) (int, []byte, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, a.baseURL+route, reader)
	if err != nil {
		return 0, nil, fmt.Errorf("%w: %w", ErrNotDispatched, err)
	}
	req.Header.Set("Authorization", "Bearer "+a.key)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := generationHTTPDo(a.client, req)
	if err != nil {
		// 不回传底层错误：其中可能带有请求地址。
		return 0, nil, fmt.Errorf("%w: aicodewith transport", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxAICodeWithTaskBytes+1))
	if err != nil || len(raw) > maxAICodeWithTaskBytes {
		return 0, nil, fmt.Errorf("%w: aicodewith answer body", ErrProtocol)
	}
	return resp.StatusCode, raw, nil
}

// outputs 以任务ID加结果文件名作稳定输出身份（文件名只是序号）；取件地址只进入受保护字段。
func (a *AICodeWithImages) outputs(task aicodewithTask) ([]GenerationOutput, error) {
	expires := a.now().Add(aicodewithResultTTL)
	out := make([]GenerationOutput, 0, len(task.ResultData))
	for i, result := range task.ResultData {
		u, err := url.Parse(result.URL)
		if err != nil || u.Scheme != "https" || u.Host == "" {
			return nil, fmt.Errorf("%w: aicodewith result location", ErrProtocol)
		}
		id := task.ID + "_" + strings.TrimSuffix(path.Base(u.Path), path.Ext(u.Path))
		if !generationToken(id) {
			return nil, fmt.Errorf("%w: aicodewith result identity", ErrProtocol)
		}
		out = append(out, GenerationOutput{ID: id, Index: i, Kind: GenerationImage, Media: &GeneratedMedia{MIME: "image/png", ByteSize: aicodewithImageMaxBytes, SizeIsLimit: true, FetchURL: result.URL, ExpiresAt: expires}})
	}
	return out, nil
}

// providerErrorCode 给日志用：结构化code或字符串错误的有界标记，不含原文。
func (t aicodewithTask) providerErrorCode() string {
	var structured struct {
		Code json.RawMessage `json:"code"`
		Type string          `json:"type"`
	}
	if json.Unmarshal(t.Error, &structured) == nil {
		return "code=" + providerToken(strings.Trim(string(structured.Code), `"`)) + " type=" + providerToken(structured.Type)
	}
	return "unstructured"
}

// errorCode 只保留中转给出的有界错误码；原文可能含提示词片段，不进入证据。
func (t aicodewithTask) errorCode() string {
	var structured struct {
		Code string `json:"code"`
	}
	if json.Unmarshal(t.Error, &structured) == nil && toolNamePattern(structured.Code) {
		return structured.Code
	}
	return "generation_failed"
}
