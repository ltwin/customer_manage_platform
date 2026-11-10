package llmgateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// KrillImages 使用异步 Images 协议；创建、查询与受控取件共用同一供应商身份。
// 凭证只发给配置的 API 源，结果重定向使用不带凭证的独立请求。
type KrillImages struct {
	client  *http.Client
	baseURL string
	key     string
}

func NewKrillImages(client *http.Client, baseURL, credential string) (*KrillImages, error) {
	u, err := url.Parse(baseURL)
	if client == nil || credential == "" || err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" && u.Path != "/" {
		return nil, fmt.Errorf("%w: krill image adapter", ErrValidation)
	}
	c := *client
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &KrillImages{client: &c, baseURL: strings.TrimRight(baseURL, "/"), key: credential}, nil
}

type krillImageTask struct {
	ID         string    `json:"id"`
	Status     string    `json:"status"`
	Model      string    `json:"model"`
	ContentURL string    `json:"content_url"`
	ExpiresAt  time.Time `json:"expires_at"`
}

func (a *KrillImages) Submit(ctx context.Context, call GenerationCall) (GenerationEvent, error) {
	route, contentType, body, err := a.requestBody(ctx, call)
	if err != nil {
		return GenerationEvent{}, fmt.Errorf("%w: %w", ErrNotDispatched, err)
	}
	status, raw, err := a.do(ctx, http.MethodPost, route, contentType, body)
	if err != nil {
		return GenerationEvent{}, err
	}
	if code, ok := map[int]string{400: "invalid_parameter", 401: "credential_refused", 402: "insufficient_balance", 403: "credential_refused"}[status]; ok {
		return GenerationEvent{EventKey: "submit", Observation: GenerationObservation{State: GenerationRejected, ErrorCode: code}, Diagnostic: fmt.Sprintf("http_status=%d", status)}, nil
	}
	// 429/5xx 不能证明未受理，不盲目重发。
	if status != http.StatusOK && status != http.StatusAccepted {
		return GenerationEvent{}, fmt.Errorf("%w: krill submit status %d", ErrProtocol, status)
	}
	var task krillImageTask
	if json.Unmarshal(raw, &task) != nil || !krillTaskID(task.ID) || task.Status != "pending" {
		return GenerationEvent{}, fmt.Errorf("%w: krill submit answer", ErrProtocol)
	}
	return GenerationEvent{EventKey: "submit", Observation: GenerationObservation{State: GenerationAccepted, TaskID: task.ID}}, nil
}

func (a *KrillImages) Observe(ctx context.Context, call GenerationCall) (GenerationEvent, error) {
	if !krillTaskID(call.TaskID) {
		return GenerationEvent{}, fmt.Errorf("%w: krill task identity", ErrValidation)
	}
	route := "/v1/images/" + call.TaskID
	status, raw, err := a.do(ctx, http.MethodGet, route, "", nil)
	if err != nil {
		return GenerationEvent{}, err
	}
	if status == http.StatusNotFound {
		return GenerationEvent{EventKey: "not_found", Observation: GenerationObservation{State: GenerationUnknown, ErrorCode: "task_not_found"}}, nil
	}
	if status != http.StatusOK {
		return GenerationEvent{}, fmt.Errorf("%w: krill query status %d", ErrProtocol, status)
	}
	var task krillImageTask
	if json.Unmarshal(raw, &task) != nil || task.ID != call.TaskID || task.Model != call.Prepared.Generation().Snapshot().Request.ModelKey {
		return GenerationEvent{}, fmt.Errorf("%w: krill task answer", ErrProtocol)
	}
	event := GenerationEvent{EventKey: task.Status, Observation: GenerationObservation{TaskID: task.ID}}
	switch task.Status {
	case "pending":
		event.Observation.State = GenerationRunning
	case "failed":
		event.Observation.State = GenerationFailed
		event.Observation.ErrorCode = "generation_failed"
	case "completed":
		content := route + "/content"
		if task.ContentURL != content && task.ContentURL != a.baseURL+content {
			return GenerationEvent{}, fmt.Errorf("%w: krill content identity", ErrProtocol)
		}
		if task.ExpiresAt.IsZero() {
			return GenerationEvent{}, fmt.Errorf("%w: krill result expiry", ErrProtocol)
		}
		event.Observation.State = GenerationCompleted
		event.Observation.Outputs = []GenerationOutput{{ID: task.ID + "_0", Index: 0, Kind: GenerationImage, Media: &GeneratedMedia{MIME: "image/png", ByteSize: aicodewithImageMaxBytes, SizeIsLimit: true, FetchURL: a.baseURL + content, ExpiresAt: task.ExpiresAt}}}
	default:
		return GenerationEvent{}, fmt.Errorf("%w: krill task state", ErrProtocol)
	}
	return event, nil
}

// Krill 未公开取消接口；仅查询，不把本地取消解释为供应商终止。
func (a *KrillImages) Cancel(ctx context.Context, call GenerationCall) (GenerationEvent, error) {
	return a.Observe(ctx, call)
}

func krillTaskID(id string) bool {
	return strings.HasPrefix(id, "img_") && len(id) > 4 && generationToken(id) && !strings.ContainsAny(id, "./")
}

func (a *KrillImages) requestBody(ctx context.Context, call GenerationCall) (string, string, []byte, error) {
	r := call.Prepared.Generation().Snapshot().Request
	if (r.ModelKey != "gpt-image-2.6-flare" && r.ModelKey != "gpt-image-2.6-sunburst") || r.Kind != GenerationImage {
		return "", "", nil, ErrCapability
	}
	var p aicodewithImageParameters
	if err := json.Unmarshal(r.Input.Parameters, &p); err != nil {
		return "", "", nil, ErrValidation
	}
	size, err := krillImageSize(p.AspectRatio, p.Resolution)
	if err != nil {
		return "", "", nil, err
	}
	fields := map[string]string{"model": r.ModelKey, "prompt": r.Input.Prompt, "size": size, "quality": p.Quality, "background": p.Background, "output_format": "png", "response_format": "url", "async": "true", "n": "1"}
	var images []GenerationReference
	for _, ref := range r.Input.References {
		if ref.Kind == GenerationImage {
			images = append(images, ref)
		}
	}
	if len(images) == 0 {
		body := map[string]any{}
		for k, v := range fields {
			body[k] = v
		}
		body["async"] = true
		body["n"] = 1
		raw, err := json.Marshal(body)
		return "/v1/images/generations", "application/json", raw, err
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for k, v := range fields {
		if err := writer.WriteField(k, v); err != nil {
			return "", "", nil, err
		}
	}
	for i, ref := range images {
		data, err := readGenerationReference(ctx, call.References, ref)
		if err != nil {
			return "", "", nil, err
		}
		header := textproto.MIMEHeader{}
		ext := map[string]string{"image/png": "png", "image/jpeg": "jpg", "image/webp": "webp"}[ref.MIME]
		if ext == "" {
			return "", "", nil, ErrCapability
		}
		header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="image[]"; filename="reference-%d.%s"`, i, ext))
		header.Set("Content-Type", ref.MIME)
		part, err := writer.CreatePart(header)
		if err != nil {
			return "", "", nil, err
		}
		if _, err := part.Write(data); err != nil {
			return "", "", nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return "", "", nil, err
	}
	return "/v1/images/edits", writer.FormDataContentType(), body.Bytes(), nil
}

// 与 Krill 创作面板一致：长边取 1K/2K/4K，再限制到 655360–8294400 像素、
// 16 的倍数。例：16:9 的三档为 1088x608、2048x1152、3840x2160。
func krillImageSize(ratio, resolution string) (string, error) {
	if ratio == "auto" {
		return "auto", nil
	}
	edge := map[string]float64{"1K": 1024, "2K": 2048, "4K": 4096}[resolution]
	parts := strings.Split(ratio, ":")
	if edge == 0 || len(parts) != 2 {
		return "", ErrValidation
	}
	x, e1 := strconv.ParseFloat(parts[0], 64)
	y, e2 := strconv.ParseFloat(parts[1], 64)
	if e1 != nil || e2 != nil || x <= 0 || y <= 0 {
		return "", ErrValidation
	}
	w, h := edge*x/math.Max(x, y), edge*y/math.Max(x, y)
	round := math.Round
	if w*h < 655360 {
		scale := math.Sqrt(655360 / (w * h))
		w *= scale
		h *= scale
		round = math.Ceil
	}
	if w*h > 8294400 {
		scale := math.Sqrt(8294400 / (w * h))
		w *= scale
		h *= scale
		round = math.Floor
	}
	return fmt.Sprintf("%dx%d", int(round(w/16))*16, int(round(h/16))*16), nil
}

func (a *KrillImages) do(ctx context.Context, method, route, contentType string, body []byte) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, a.baseURL+route, bytes.NewReader(body))
	if err != nil {
		return 0, nil, fmt.Errorf("%w: krill request", ErrNotDispatched)
	}
	req.Header.Set("Authorization", "Bearer "+a.key)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := generationHTTPDo(a.client, req)
	if err != nil {
		return 0, nil, fmt.Errorf("krill transport: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxAICodeWithTaskBytes+1))
	if err != nil || len(raw) > maxAICodeWithTaskBytes {
		return 0, nil, fmt.Errorf("%w: krill answer body", ErrProtocol)
	}
	return resp.StatusCode, raw, nil
}

var _ GenerationAdapter = (*KrillImages)(nil)
