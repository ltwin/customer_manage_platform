package creativeagent

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"unicode/utf8"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/samson/customer-manage-platform/backend/internal/creativeskill"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const searchSkillsTool = "search_skills"
const skillDiscoveryBytes = 16 << 10

// This is a frozen discovery page, never a live pointer to a recommended version.
type skillDiscoveryPage struct {
	Items      []skillIndexEntry `json:"items"`
	NextCursor string            `json:"next_cursor"`
}

type skillTool struct {
	handler *runtimeHandler
	name    string
}

func (t *skillTool) Info(context.Context) (*schema.ToolInfo, error) {
	params := map[string]*schema.ParameterInfo{}
	desc := "加载已发现或本会话用过的固定 Skill 版本，返回方法和资源清单；不会增加业务工具权限。"
	switch t.name {
	case searchSkillsTool:
		desc = "按名称或描述查找可用 Skill 方法。返回固定版本摘要，不加载正文。空 items 且 next_cursor 非空时继续翻页；每页最多 20 条。"
		params["query"] = &schema.ParameterInfo{Type: schema.String, Desc: "搜索词，最多 64 字符；空串列出目录"}
		params["cursor"] = &schema.ParameterInfo{Type: schema.String, Desc: "上一页 next_cursor；首次留空"}
	case readSkillTool:
		desc = "读取本轮已激活 Skill 的声明文字资源；先用 load_skill 加载该版本。"
		params["skill_version_id"] = &schema.ParameterInfo{Type: schema.String, Required: true, Desc: "已激活的固定版本 ID"}
		params["path"] = &schema.ParameterInfo{Type: schema.String, Required: true, Desc: "load_skill 返回清单中的相对路径"}
	default:
		params["skill_version_id"] = &schema.ParameterInfo{Type: schema.String, Required: true, Desc: "目录或会话索引给出的固定版本 ID"}
	}
	return &schema.ToolInfo{Name: t.name, Desc: desc, ParamsOneOf: schema.NewParamsOneOfByParams(params)}, nil
}
func (t *skillTool) InvokableRun(ctx context.Context, raw string, _ ...tool.Option) (string, error) {
	switch t.name {
	case searchSkillsTool:
		var args struct {
			Query  string `json:"query"`
			Cursor string `json:"cursor"`
		}
		if err := strictArguments(raw, &args); err != nil {
			return "", err
		}
		step := toolStepFrom(ctx)
		if step == "" {
			return "", ErrRunState
		}
		page, err := t.handler.discoverSkills(ctx, args.Query, args.Cursor, step)
		if err != nil {
			return "", err
		}
		b, err := json.Marshal(page)
		return string(b), err
	case readSkillTool:
		return t.handler.readSkillResource(ctx, raw)
	default:
		var args struct {
			VersionID string `json:"skill_version_id"`
		}
		if err := strictArguments(raw, &args); err != nil {
			return "", err
		}
		snapshot, err := t.handler.activateSkill(ctx, args.VersionID)
		if err != nil {
			return "", err
		}
		return skillContent(snapshot)
	}
}
func skillContent(s creativeskill.Snapshot) (string, error) {
	return marshalToolResult(struct {
		Version      string                   `json:"skill_version_id"`
		Name         string                   `json:"name"`
		Instructions string                   `json:"instructions"`
		Resources    []creativeskill.Resource `json:"resources"`
		Guidance     string                   `json:"output_guidance,omitempty"`
	}{s.ID, s.DisplayName, s.Instructions, s.Resources, s.Manifest.OutputGuidance})
}
func skillEntry(s creativeskill.Snapshot) skillIndexEntry {
	return skillIndexEntry{SkillID: s.SkillID, VersionID: s.ID, VersionNumber: s.VersionNumber, Digest: s.Digest, DisplayName: s.DisplayName, Description: s.Description, Origin: s.Origin}
}

func modelSupportsSkill(m creativeskill.Manifest, capability llmgateway.Capability) string {
	for _, required := range m.RequiredModelCapabilities {
		supported := false
		switch required {
		case "tool_calling":
			supported = capability.ToolCalling
		case "image_input":
			supported = capability.ImageInput
		case "tool_choice":
			supported = capability.ToolChoice
		case "json_object":
			supported = capability.JSONObject
		case "temperature":
			supported = capability.Temperature
		}
		if !supported {
			return "当前模型不支持 Skill 所需能力：" + required
		}
	}
	return ""
}
func dependencyRef(ref string) string {
	switch ref {
	case "load_skill@1":
		return "load_skill@2"
	case "read_skill_resource@1":
		return "read_skill_resource@2"
	}
	return ref
}
func (h *runtimeHandler) skillUnavailable(s creativeskill.Snapshot) string {
	if !s.Usable() {
		return "该 Skill 版本已停用"
	}
	if reason := modelSupportsSkill(s.Manifest, h.held.Model.Capability); reason != "" {
		return reason
	}
	refs := map[string]bool{}
	for _, d := range agentToolDefinitions() {
		if h.allowed(d.entry.Key) {
			refs[toolRef(d.entry.Key, d.entry.Version)] = true
		}
	}
	for _, ref := range s.Manifest.Dependencies() {
		if !refs[dependencyRef(ref)] {
			return "本轮未开放 Skill 所需工具：" + ref
		}
	}
	return ""
}

// registerSkillInTx runs under the run lock. Repeated discovery cannot replace
// a candidate or active snapshot that has already been shown to the model.
func registerSkillInTx(ctx context.Context, tx store.TxAccountScope, runID, source string, e skillIndexEntry) error {
	var digest string
	err := tx.QueryRow(ctx, "creative_run_skills", "digest", "run_id=$2 AND skill_version_id=$3", runID, e.VersionID).Scan(&digest)
	if err == nil {
		if digest != e.Digest {
			return creativeskill.ErrContentMismatch
		}
		return nil
	}
	if !errors.Is(err, store.ErrNoRows) {
		return err
	}
	raw, err := json.Marshal(e)
	if err != nil {
		return err
	}
	return tx.Insert(ctx, "creative_run_skills", []string{"run_id", "skill_id", "skill_version_id", "digest", "source", "metadata"}, runID, e.SkillID, e.VersionID, e.Digest, source, raw)
}
func (h *runtimeHandler) readDiscovery(ctx context.Context, tx store.TxAccountScope, step string) ([]byte, error) {
	var raw []byte
	var err error
	if step == "" {
		err = tx.QueryRow(ctx, "creative_agent_runs", "skill_discovery", "id=$2", h.held.RunID).Scan(&raw)
	} else {
		err = tx.QueryRow(ctx, "creative_run_skill_searches", "result", "run_id=$2 AND step_id=$3", h.held.RunID, step).Scan(&raw)
	}
	if errors.Is(err, store.ErrNoRows) {
		return nil, nil
	}
	return raw, err
}
func (h *runtimeHandler) discoverSkills(ctx context.Context, query, cursor, step string) (skillDiscoveryPage, error) {
	var saved []byte
	err := h.scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		var err error
		saved, err = h.readDiscovery(ctx, tx, step)
		return err
	})
	if err != nil {
		return skillDiscoveryPage{}, err
	}
	if len(saved) > 0 {
		var page skillDiscoveryPage
		err = json.Unmarshal(saved, &page)
		return page, err
	}
	found, err := h.service.skills.ListAccessibleSkills(ctx, h.scope, query, cursor, 20)
	if err != nil {
		return skillDiscoveryPage{}, err
	}
	page := skillDiscoveryPage{Items: []skillIndexEntry{}, NextCursor: found.NextCursor}
	for _, entry := range found.Items {
		s, err := h.service.skills.ResolveVersion(ctx, h.scope, entry.SkillID, entry.VersionID)
		if errors.Is(err, creativeskill.ErrNotFound) {
			continue
		}
		if err != nil {
			return page, err
		}
		if s.Digest != entry.Digest || h.skillUnavailable(s) != "" {
			continue
		}
		e := skillEntry(s)
		// Bound each page, not just its item count; search remains available for
		// candidates omitted from an initial summary.
		e.Description = utf8Prefix(e.Description, 192)
		e.DisplayName = utf8Prefix(e.DisplayName, 96)
		page.Items = append(page.Items, e)
	}
	raw, err := json.Marshal(page)
	if err != nil {
		return page, err
	}
	err = h.scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := h.service.admitRunInTx(ctx, tx, h.held); err != nil {
			return err
		}
		prior, err := h.readDiscovery(ctx, tx, step)
		if err != nil {
			return err
		}
		if len(prior) > 0 {
			return json.Unmarshal(prior, &page)
		}
		source := "search"
		if step == "" {
			source = "catalog"
		}
		for _, entry := range page.Items {
			if err := registerSkillInTx(ctx, tx, h.held.RunID, source, entry); err != nil {
				return err
			}
		}
		if step == "" {
			_, err = tx.Update(ctx, "creative_agent_runs", "skill_discovery=$3", "id=$2", h.held.RunID, raw)
			return err
		}
		return tx.Insert(ctx, "creative_run_skill_searches", []string{"run_id", "step_id", "result"}, h.held.RunID, step, raw)
	})
	return page, err
}
func (h *runtimeHandler) initializeSkills(ctx context.Context) (string, error) {
	if !h.held.Model.Capability.ToolCalling {
		return "", nil
	}
	err := h.scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := h.service.admitRunInTx(ctx, tx, h.held); err != nil {
			return err
		}
		for _, e := range h.skillIndex {
			if err := registerSkillInTx(ctx, tx, h.held.RunID, "history", e); err != nil {
				return err
			}
		}
		if s := h.held.Skill; s != nil {
			return registerSkillInTx(ctx, tx, h.held.RunID, "explicit", skillEntry(*s))
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if s := h.held.Skill; s != nil {
		if _, err := h.activateSkill(ctx, s.ID); err != nil {
			return "", err
		}
	}
	page, err := h.discoverSkills(ctx, "", "", "")
	if err != nil {
		return "", err
	}
	return skillDiscoveryText(page, h.held.Skill)
}

// Trim only the optional directory, using encoded bytes. The frozen candidate
// identities remain registered and search can retrieve anything omitted here.
func skillDiscoveryText(page skillDiscoveryPage, selected *creativeskill.Snapshot) (string, error) {
	prefix := "按任务选择相关 Skill；先用 load_skill 加载方法，再按清单读取资源。手动指定的方法优先。目录未覆盖时用 search_skills 按名称或描述查找。加载不会改变业务权限。"
	if selected != nil {
		active, err := json.Marshal(struct {
			Version   string                   `json:"skill_version_id"`
			Resources []creativeskill.Resource `json:"resources"`
		}{selected.ID, selected.Resources})
		if err != nil {
			return "", err
		}
		prefix += "\n本轮已激活：" + string(active)
	}
	for {
		raw, err := json.Marshal(page)
		if err != nil {
			return "", err
		}
		text := prefix + "\nSkill 目录：" + string(raw)
		if len(text) <= skillDiscoveryBytes {
			return text, nil
		}
		if len(page.Items) == 0 {
			// An unusually escaped resource list is also optional: load_skill is
			// idempotent and can retrieve the full list for this activated ID.
			if selected != nil {
				return "本轮已激活 Skill：" + selected.ID + "；正文已提供。用 load_skill 获取资源清单，用 search_skills 查找其他方法。", nil
			}
			return "", ErrLimit
		}
		page.Items = page.Items[:len(page.Items)-1]
	}
}

const skillHistoryUnloaded = "历史 Skill 方法或资源本轮不可用；需要沿用时按会话索引重新加载固定版本。"

func skillHistoryTool(name string) bool { return name == loadSkillTool || name == readSkillTool }

// historySkill is one method or resource result still inside the assembled
// history, with the version its call named.
type historySkill struct {
	message *schema.Message
	version string
}

// carrySkills keeps a method an earlier run of the conversation activated
// active in this run while its body is still in history: loaded once, not on
// every turn, until compaction drops it (shoot-plan design §6.6). Activation
// rechecks the version, so a withdrawn or unrunnable one is not resent.
func (h *runtimeHandler) carrySkills(ctx context.Context, history []historySkill) error {
	carried := map[string]bool{}
	for _, item := range history {
		ok, seen := carried[item.version]
		if !seen {
			var err error
			if ok, err = h.carrySkill(ctx, item.version); err != nil {
				return err
			}
			carried[item.version] = ok
		}
		if !ok {
			item.message.Content = skillHistoryUnloaded
		}
	}
	return nil
}

func (h *runtimeHandler) carrySkill(ctx context.Context, version string) (bool, error) {
	if !h.held.Model.Capability.ToolCalling {
		return false, nil
	}
	found := false
	err := h.scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := h.service.admitRunInTx(ctx, tx, h.held); err != nil {
			return err
		}
		// Only a version some run of this conversation really activated; a
		// refused load in history grants nothing.
		var raw []byte
		err := tx.QueryRow(ctx, "creative_run_skills", "metadata",
			"skill_version_id=$2 AND activated_at IS NOT NULL AND run_id IN (SELECT id FROM creative_agent_runs WHERE account_id=$1 AND conversation_id=$3)",
			version, h.held.ConversationID).Scan(&raw)
		if errors.Is(err, store.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		var e skillIndexEntry
		if err := json.Unmarshal(raw, &e); err != nil {
			return err
		}
		found = true
		return registerSkillInTx(ctx, tx, h.held.RunID, "history", e)
	})
	if err != nil || !found {
		return false, err
	}
	_, err = h.activateSkill(ctx, version)
	var refused *toolRefusal
	if errors.As(err, &refused) || errors.Is(err, creativeskill.ErrContentMismatch) {
		return false, nil
	}
	return err == nil, err
}

func (h *runtimeHandler) activateSkill(ctx context.Context, version string) (creativeskill.Snapshot, error) {
	var entry skillIndexEntry
	err := h.scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		var raw []byte
		if err := tx.QueryRow(ctx, "creative_run_skills", "metadata", "run_id=$2 AND skill_version_id=$3", h.held.RunID, version).Scan(&raw); err != nil {
			return err
		}
		return json.Unmarshal(raw, &entry)
	})
	if errors.Is(err, store.ErrNoRows) {
		return creativeskill.Snapshot{}, refuseTool("请先用 search_skills 查找这个 Skill 的固定版本")
	}
	if err != nil {
		return creativeskill.Snapshot{}, err
	}
	live, err := h.service.skills.ResolveVersion(ctx, h.scope, entry.SkillID, entry.VersionID)
	if errors.Is(err, creativeskill.ErrNotFound) {
		return live, refuseTool("这个 Skill 版本已不可访问")
	}
	if err != nil {
		return live, err
	}
	if why := h.skillUnavailable(live); why != "" {
		return live, refuseTool("%s", why)
	}
	if live.Digest != entry.Digest {
		return live, creativeskill.ErrContentMismatch
	}
	raw, err := json.Marshal(live)
	if err != nil {
		return live, err
	}
	// Check the complete response bound before making activation durable.
	body, err := skillContent(live)
	if err != nil {
		return live, err
	}
	if len(body) > h.service.limits.ToolResultBytes {
		return live, refuseTool("Skill 正文与资源清单超过本轮读取上限")
	}
	err = h.scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		// Include the new version in the sorted control locks, before consent.
		next := h.held
		next.Skill = &live
		if err := h.service.admitRunInTx(ctx, tx, next); err != nil {
			return err
		}
		var prior []byte
		if err := tx.QueryRow(ctx, "creative_run_skills", "snapshot", "run_id=$2 AND skill_version_id=$3", h.held.RunID, version).Scan(&prior); err != nil {
			return err
		}
		if len(prior) > 0 {
			return json.Unmarshal(prior, &live)
		}
		count, err := tx.Count(ctx, "creative_run_skills", "run_id=$2 AND activated_at IS NOT NULL", h.held.RunID)
		if err != nil {
			return err
		}
		if count >= int64(h.service.limits.ActivatedSkillsPerRun) {
			return refuseTool("本轮激活 Skill 数量已达上限")
		}
		_, err = tx.Update(ctx, "creative_run_skills", "snapshot=$4,activated_at=clock_timestamp()", "run_id=$2 AND skill_version_id=$3", h.held.RunID, version, raw)
		return err
	})
	return live, err
}
func (h *runtimeHandler) readSkillResource(ctx context.Context, raw string) (string, error) {
	var args struct {
		Version string `json:"skill_version_id"`
		Path    string `json:"path"`
	}
	if err := strictArguments(raw, &args); err != nil {
		return "", err
	}
	var snapshot creativeskill.Snapshot
	err := h.scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := h.service.admitRunInTx(ctx, tx, h.held); err != nil {
			return err
		}
		var b []byte
		if err := tx.QueryRow(ctx, "creative_run_skills", "snapshot", "run_id=$2 AND skill_version_id=$3 AND activated_at IS NOT NULL", h.held.RunID, args.Version).Scan(&b); err != nil {
			return err
		}
		return json.Unmarshal(b, &snapshot)
	})
	if errors.Is(err, store.ErrNoRows) {
		return "", refuseTool("请先加载这个固定 Skill 版本，再读取它的资源")
	}
	if err != nil {
		return "", err
	}
	expected := ""
	for _, r := range snapshot.Resources {
		if r.Path == args.Path {
			expected = r.SHA256
		}
	}
	if expected == "" {
		return "", ErrNotFound
	}
	b, err := h.service.skills.ReadResource(ctx, h.scope, snapshot.SkillID, snapshot.ID, args.Path)
	if err != nil {
		return "", err
	}
	if digestBytes(b) != expected {
		return "", creativeskill.ErrContentMismatch
	}
	if !utf8.Valid(b) {
		return "", creativeops.ErrValidation
	}
	return string(b), nil
}

// Control locks are always taken in version order, after the run and before
// consent/content locks. A newly activated version joins the same ordering.
func (s *Service) requireActiveSkillsInTx(ctx context.Context, tx store.TxAccountScope, held claim) error {
	pairs := map[string]string{}
	if held.Skill != nil {
		pairs[held.Skill.ID] = held.Skill.SkillID
	}
	rows, err := tx.Query(ctx, "creative_run_skills", "skill_id,skill_version_id", "run_id=$2 AND activated_at IS NOT NULL", held.RunID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var skill, version string
		if err := rows.Scan(&skill, &version); err != nil {
			rows.Close()
			return err
		}
		pairs[version] = skill
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	versions := make([]string, 0, len(pairs))
	for v := range pairs {
		versions = append(versions, v)
	}
	sort.Strings(versions)
	for _, v := range versions {
		if err := s.skills.RequireRunnableInTx(ctx, tx, pairs[v], v); err != nil {
			return translateSkill(err)
		}
	}
	return nil
}
