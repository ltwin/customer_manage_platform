package creativeagent

import "encoding/json"

// These tools can return source bytes. Their results must retain provenance
// through paging, history and checkpoint recovery, not merely an opaque ID.
func contentReadTool(name string) bool {
	return name == readAssetTool || name == readCanvasTool || name == readResultTool || name == readHistoryTool
}

func toolContentRevisions(name, raw string) []string {
	if !contentReadTool(name) && name != generateNodeTool && name != compressImageTool && name != renderViewTool {
		return nil
	}
	var result struct {
		ID         string   `json:"content_revision_id"`
		IDs        []string `json:"content_revision_ids"`
		ApplyState string   `json:"apply_state"`
		State      string   `json:"state"`
	}
	if json.Unmarshal([]byte(raw), &result) != nil {
		return nil
	}
	if name == generateNodeTool && result.ApplyState != "applied" || name == compressImageTool && result.State != "applied" {
		return nil
	}
	if result.ID != "" {
		result.IDs = append(result.IDs, result.ID)
	}
	ids := []string{}
	seen := map[string]bool{}
	for _, id := range result.IDs {
		if id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids
}
