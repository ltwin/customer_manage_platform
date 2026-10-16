package llmgateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// RunningHubImages 只翻译标准Model API，不接工作流，不自行重试或轮询。
type RunningHubImages struct {
	client     *http.Client
	baseURL    string
	credential func() (string, bool)
}

var _ GenerationAdapter = (*RunningHubImages)(nil)

func NewRunningHubImages(client *http.Client, baseURL string, credential func() (string, bool)) (*RunningHubImages, error) {
	u, err := url.Parse(baseURL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || client == nil || credential == nil {
		return nil, ErrValidation
	}
	c := *client
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &RunningHubImages{client: &c, baseURL: strings.TrimRight(baseURL, "/"), credential: credential}, nil
}
func RunningHubImageHosts() []string {
	return []string{"rh-hk-images-1252422369.cos.ap-hongkong.myqcloud.com", "rh-images-1252422369.cos.ap-beijing.myqcloud.com", "rh-images-switch-1252422369.cos.ap-guangzhou.myqcloud.com", "rh-images.xiaoyaoyou.com"}
}
func runningHubURL(raw string, hosts []string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" || u.Port() != "" && u.Port() != "443" {
		return false
	}
	for _, host := range hosts {
		if strings.EqualFold(u.Hostname(), host) {
			return true
		}
	}
	return false
}

type runningHubTask struct {
	TaskID    string `json:"taskId"`
	Status    string `json:"status"`
	ErrorCode string `json:"errorCode"`
	Results   []struct {
		URL        string `json:"url"`
		OutputType string `json:"outputType"`
	} `json:"results"`
}

func (a *RunningHubImages) Submit(ctx context.Context, call GenerationCall) (GenerationEvent, error) {
	body, path, err := a.requestBody(ctx, call)
	if err != nil {
		return GenerationEvent{}, fmt.Errorf("%w: %w", ErrNotDispatched, err)
	}
	// 上传共用有界阶段，但最终创建任务至少保留15秒；不足时尚未创建生成任务。
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < 15*time.Second {
		return GenerationEvent{}, fmt.Errorf("%w: insufficient submission time after uploads (%s left)", ErrNotDispatched, time.Until(deadline).Round(time.Second))
	}
	status, raw, err := a.do(ctx, path, "application/json", body)
	if err != nil {
		return GenerationEvent{}, fmt.Errorf("runninghub submit: %w", err)
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden || status == http.StatusPaymentRequired {
		code := "credential_refused"
		if status == http.StatusPaymentRequired {
			code = "insufficient_balance"
		}
		return GenerationEvent{EventKey: "submit", Observation: GenerationObservation{State: GenerationRejected, ErrorCode: code}, Diagnostic: fmt.Sprintf("http_status=%d", status)}, nil
	}
	var t runningHubTask
	if err = json.Unmarshal(raw, &t); err != nil {
		return GenerationEvent{}, fmt.Errorf("%w: runninghub submit answer status=%d not decodable", ErrProtocol, status)
	}
	diagnostic := fmt.Sprintf("http_status=%d provider_error=%s", status, providerToken(t.ErrorCode))
	// 只有明确的未受理错误才归零；5xx、空任务和未知错误保留受理未知。
	// 限流类（415实例占满、421共享并发、1003频率、1011繁忙）官方要求客户端排队
	// 重试，且没有任务ID；已受理的排队/运行中是813/804，不在此列。
	if t.TaskID == "" {
		code := runningHubError(t.ErrorCode)
		if code == "credential_refused" || code == "insufficient_balance" || code == "invalid_parameter" || code == "content_rejected" || code == "rate_limited" {
			return GenerationEvent{EventKey: "submit", Observation: GenerationObservation{State: GenerationRejected, ErrorCode: code}, Diagnostic: diagnostic}, nil
		}
		return GenerationEvent{}, fmt.Errorf("%w: runninghub submission outcome %s", ErrProtocol, diagnostic)
	}
	if status != http.StatusOK {
		return GenerationEvent{}, fmt.Errorf("%w: runninghub submit %s", ErrProtocol, diagnostic)
	}
	return a.event(t)
}
func (a *RunningHubImages) Observe(ctx context.Context, call GenerationCall) (GenerationEvent, error) {
	if call.TaskID == "" {
		return GenerationEvent{}, ErrValidation
	}
	raw, err := json.Marshal(map[string]string{"taskId": call.TaskID})
	if err != nil {
		return GenerationEvent{}, err
	}
	status, raw, err := a.do(ctx, "/openapi/v2/query", "application/json", raw)
	if err != nil {
		return GenerationEvent{}, fmt.Errorf("runninghub query: %w", err)
	}
	var t runningHubTask
	if status != http.StatusOK || json.Unmarshal(raw, &t) != nil || t.TaskID != call.TaskID {
		return GenerationEvent{}, fmt.Errorf("%w: runninghub query answer status=%d task_matches=%t", ErrProtocol, status, t.TaskID == call.TaskID)
	}
	return a.event(t)
}

// 标准模型的取消能力未经确认；查询同一任务，不宣称远端已取消。
func (a *RunningHubImages) Cancel(ctx context.Context, call GenerationCall) (GenerationEvent, error) {
	return a.Observe(ctx, call)
}
func (a *RunningHubImages) event(t runningHubTask) (GenerationEvent, error) {
	e := GenerationEvent{Observation: GenerationObservation{TaskID: t.TaskID}}
	switch t.Status {
	case "QUEUED", "RUNNING":
		e.EventKey = "running"
		e.Observation.State = GenerationRunning
	case "FAILED":
		e.EventKey = "failed"
		e.Observation.State = GenerationFailed
		e.Observation.ErrorCode = runningHubError(t.ErrorCode)
		e.Diagnostic = "provider_error=" + providerToken(t.ErrorCode)
	case "SUCCESS":
		if len(t.Results) != 1 {
			return GenerationEvent{}, fmt.Errorf("%w: runninghub result count %d", ErrProtocol, len(t.Results))
		}
		e.EventKey = "completed"
		e.RefreshableMedia = true
		e.Observation.State = GenerationCompleted
		for i, out := range t.Results {
			if !runningHubURL(out.URL, RunningHubImageHosts()) {
				return GenerationEvent{}, fmt.Errorf("%w: runninghub output location", ErrProtocol)
			}
			mime := map[string]string{"png": "image/png", "jpg": "image/jpeg", "jpeg": "image/jpeg", "webp": "image/webp"}[strings.ToLower(out.OutputType)]
			if mime == "" {
				return GenerationEvent{}, fmt.Errorf("%w: runninghub output type %s", ErrProtocol, providerToken(out.OutputType))
			}
			// 一次任务单张输出；身份不依赖可轮换的签名URL。
			id := fmt.Sprintf("image-%d", i)
			e.Observation.Outputs = append(e.Observation.Outputs, GenerationOutput{ID: id, Index: i, Kind: GenerationImage, Media: &GeneratedMedia{MIME: mime, ByteSize: 25 << 20, SizeIsLimit: true, FetchURL: out.URL, ExpiresAt: time.Now().Add(15 * time.Minute)}})
		}
	default:
		return GenerationEvent{}, fmt.Errorf("%w: runninghub task state %s", ErrProtocol, providerToken(t.Status))
	}
	// 官方query没有费用维度：保持Usage为空，由真实账单完成核算。
	return e, nil
}
func runningHubError(code string) string {
	switch code {
	case "412", "802", "811", "1002", "1014":
		return "credential_refused"
	case "416", "812":
		return "insufficient_balance"
	case "301", "433", "803", "809", "1007", "1008", "1009", "1101":
		return "invalid_parameter"
	case "1501", "1505":
		return "content_rejected"
	case "415", "421", "1003", "1011":
		return "rate_limited"
	case "1006", "1504":
		return "execution_timeout"
	case "1005", "1010", "1012":
		return "server_error"
	default:
		return "generation_failed"
	}
}
func (a *RunningHubImages) requestBody(ctx context.Context, call GenerationCall) ([]byte, string, error) {
	req := call.Prepared.Generation().Snapshot().Request
	if req.Kind != GenerationImage || call.Prepared.PolicyDefinition().AdapterVersion != RunningHubImageAdapterVersion {
		return nil, "", ErrCapability
	}
	var p struct {
		AspectRatio string `json:"aspect_ratio"`
		Resolution  string `json:"resolution"`
		Quality     string `json:"quality"`
		Count       int    `json:"count"`
		Background  string `json:"background"`
	}
	if json.Unmarshal(req.Input.Parameters, &p) != nil || p.Count != 1 || p.Background != "auto" {
		return nil, "", fmt.Errorf("%w: runninghub image parameters", ErrCapability)
	}
	modelPath, edit := "", "image-to-image"
	switch req.ModelKey {
	case "gpt-image-2":
		modelPath = "rhart-image-g-2-official"
	case "nano-banana-pro":
		modelPath = "rhart-image-n-pro-official"
		edit = "edit"
	case "nano-banana-2":
		modelPath = "rhart-image-n-g31-flash-official"
	case "nano-banana-2.1":
		modelPath = "rhart-image-n-nb21"
	default:
		return nil, "", fmt.Errorf("%w: runninghub model %s", ErrCapability, req.ModelKey)
	}
	body := map[string]any{"prompt": req.Input.Prompt, "aspectRatio": p.AspectRatio, "resolution": strings.ToLower(p.Resolution)}
	if req.ModelKey == "gpt-image-2" {
		body["quality"] = p.Quality
	}
	var urls []string
	for i, ref := range req.Input.References {
		if ref.Kind == GenerationText {
			continue
		}
		data, err := readGenerationReference(ctx, call.References, ref)
		if err != nil {
			return nil, "", fmt.Errorf("reference %d (%s) read: %w", i, ref.RevisionID, err)
		}
		started := time.Now()
		location, err := a.upload(ctx, data, ref.MIME)
		if err != nil {
			return nil, "", fmt.Errorf("reference %d (%s, %s, %d bytes) upload after %s: %w", i, ref.RevisionID, ref.MIME, len(data), time.Since(started).Round(time.Millisecond), err)
		}
		urls = append(urls, location)
	}
	operation := "text-to-image"
	if len(urls) > 0 {
		operation = edit
		body["imageUrls"] = urls
	}
	raw, err := json.Marshal(body)
	return raw, "/openapi/v2/" + modelPath + "/" + operation, err
}
func (a *RunningHubImages) upload(ctx context.Context, data []byte, mime string) (string, error) {
	ext := map[string]string{"image/png": "png", "image/jpeg": "jpg", "image/webp": "webp"}[mime]
	if ext == "" || len(data) > 25<<20 {
		return "", fmt.Errorf("%w: runninghub upload format", ErrCapability)
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "reference."+ext)
	if err != nil {
		return "", err
	}
	if _, err = part.Write(data); err != nil {
		return "", err
	}
	if err = writer.Close(); err != nil {
		return "", err
	}
	status, raw, err := a.do(ctx, "/openapi/v2/media/upload/binary", writer.FormDataContentType(), body.Bytes())
	if err != nil {
		return "", fmt.Errorf("runninghub upload: %w", err)
	}
	var out struct {
		Code *int `json:"code"`
		Data struct {
			Type string `json:"type"`
			URL  string `json:"download_url"`
		} `json:"data"`
	}
	// 香港区上传地址已实测；不据此扩展生成结果的取件白名单。
	uploadHosts := append(RunningHubImageHosts(), "rh-hk-images-switch.xiaoyaoyou.com")
	if status != http.StatusOK || json.Unmarshal(raw, &out) != nil {
		return "", fmt.Errorf("%w: runninghub upload answer status=%d", ErrProtocol, status)
	}
	if out.Code == nil || *out.Code != 0 {
		code := "none"
		if out.Code != nil {
			code = fmt.Sprint(*out.Code)
		}
		return "", fmt.Errorf("%w: runninghub upload answer code=%s", ErrProtocol, code)
	}
	if out.Data.Type != "image" || !runningHubURL(out.Data.URL, uploadHosts) {
		return "", fmt.Errorf("%w: runninghub upload answer type=%s location=%s", ErrProtocol, providerToken(out.Data.Type), locationOrigin(out.Data.URL))
	}
	return out.Data.URL, nil
}
func (a *RunningHubImages) do(ctx context.Context, path, contentType string, body []byte) (int, []byte, error) {
	key, ok := a.credential()
	if !ok || key == "" {
		return 0, nil, fmt.Errorf("%w: runninghub credential unavailable", ErrNotDispatched)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return 0, nil, fmt.Errorf("%w: runninghub request", ErrNotDispatched)
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", contentType)
	resp, err := generationHTTPDo(a.client, req)
	if err != nil {
		return 0, nil, fmt.Errorf("%w: runninghub transport", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 {
		return 0, nil, fmt.Errorf("%w: runninghub answer body status=%d unreadable or too large", ErrProtocol, resp.StatusCode)
	}
	return resp.StatusCode, raw, nil
}

// locationOrigin 只保留供应商地址的scheme与主机，路径和查询可能含签名，不进日志。
func locationOrigin(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "unparseable"
	}
	origin := u.Scheme + "://" + u.Host
	if len(origin) > 128 {
		return origin[:128]
	}
	return origin
}
