package creativeagent

import (
	"context"
	"time"

	"github.com/cloudwego/eino/schema"

	"github.com/samson/customer-manage-platform/backend/internal/creativeimageedit"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const compressImageTool = "compress_image"

// compressWait bounds one compress_image call (backend design §5.3). The
// encode itself is bounded by the candidate's own deadline.
const compressWait = 90 * time.Second

// ImageCompressor is the image edit entry the assistant shares with the
// photographer's panel: same candidate, same adoption command, same refusals.
type ImageCompressor interface {
	CompressForAgent(context.Context, store.AccountScope, creativeimageedit.AgentCompress) (creativeimageedit.AgentCompressResult, error)
}

// SetImageCompressor binds image editing at composition time. A deployment
// with editing switched off passes nil and never offers the tool.
func (s *Service) SetImageCompressor(c ImageCompressor) {
	s.compressor = c
}

func compressImageInfo() *schema.ToolInfo {
	return &schema.ToolInfo{
		Name: compressImageTool,
		Desc: "按输出质量压缩一张图片节点，像素尺寸不变，结果作为该节点的新版本并切换显示，原图保留在版本列表里，创作者可以撤销。只按质量压缩，不能保证压到指定大小；创作者要求目标大小时如实说明。JPEG 输出 JPEG；不透明 PNG 转 JPEG；透明 PNG 和 WebP 输出 WebP；输出不保留拍摄信息（EXIF/GPS）。不知道要压哪张图、或选中了多张图时先问清楚，不要自行挑选。调用会等待结果：state 为 applied 才表示节点已更新，并带实际的原始大小、输出大小和减少比例；not_reduced 表示文件没有变小、没有改动；not_applied 和 failed 表示没有改动，reason 说明原因；unconfirmed 表示采用已提交但结果未确认，不要重复调用，请读取画布核实。",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"node_id":             {Type: schema.String, Required: true, Desc: "要压缩的图片节点 ID"},
			"quality":             {Type: schema.Integer, Desc: "输出质量，1 到 100 的整数；高质量 90、均衡 80、小文件 60；省略时为 80。质量值不是保留画质的百分比，也不是缩小比例"},
			"content_revision_id": {Type: schema.String, Desc: "read_canvas 读到的该节点图片的内容修订 ID；给出时，图片在此之后被改动就拒绝，避免压错版本"},
		}),
	}
}

func (h *runtimeHandler) compressImage(ctx context.Context, canvasID, seed, raw string) (any, error) {
	var args struct {
		NodeID            string `json:"node_id"`
		Quality           *int   `json:"quality"`
		ContentRevisionID string `json:"content_revision_id"`
	}
	if err := strictArguments(raw, &args); err != nil {
		return nil, err
	}
	in := creativeimageedit.AgentCompress{Seed: seed, CanvasID: canvasID, NodeID: args.NodeID, SourceRevisionID: args.ContentRevisionID}
	if args.Quality != nil {
		if *args.Quality < 1 || *args.Quality > 100 {
			return nil, refuseTool("质量须是 1 到 100 的整数")
		}
		in.Quality = *args.Quality
	}
	// The wait never outlives the run: the answer still has to be written.
	in.Wait = min(h.service.compressWait, time.Until(h.held.Deadline)-45*time.Second)
	result, err := h.service.compressor.CompressForAgent(ctx, h.scope, in)
	if err != nil {
		return nil, err
	}
	return struct {
		Success bool `json:"success"`
		creativeimageedit.AgentCompressResult
	}{result.State == creativeimageedit.AgentApplied, result}, nil
}
