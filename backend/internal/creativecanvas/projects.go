package creativecanvas

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// Project sorts and the cover sample bound (design 2026-10-11-creative-home §3.3).
const (
	SortEditedDesc = "edited_desc"
	SortEditedAsc  = "edited_asc"
	SortName       = "name"

	coverNodes = 60
	coverEdges = 60
	maxQuery   = 100
)

// Run and execution states that make a project "running" or "waiting". They
// match the partial indexes of migration 0060.
const (
	liveRunStates      = "'queued','running','reconciling'"
	waitingRunStates   = "'waiting_input','waiting_apply'"
	liveExecutionState = "'queued','running','reconciling'"
)

type Project struct {
	ID              string               `json:"id"`
	Name            string               `json:"name"`
	DefaultCanvasID string               `json:"default_canvas_id"`
	Revision        creativeops.Revision `json:"revision"`
	Archived        bool                 `json:"archived"`
	UpdatedAt       time.Time            `json:"updated_at"`
	EditedAt        time.Time            `json:"edited_at"`
	// Running gates archiving and polling; Waiting is only shown. The card
	// decides which label wins, so the two are never folded into one value.
	Running bool         `json:"running"`
	Waiting bool         `json:"waiting"`
	Cover   ProjectCover `json:"cover"`

	normalizedName string
}

// ProjectCover is a bounded sketch of the default canvas: geometry and kinds
// only, never text.
type ProjectCover struct {
	Nodes []CoverNode `json:"nodes"`
	Edges []CoverEdge `json:"edges"`
	More  bool        `json:"more"`
}
type CoverNode struct {
	ID                string  `json:"id"`
	X                 float64 `json:"x"`
	Y                 float64 `json:"y"`
	Width             float64 `json:"width"`
	Height            float64 `json:"height"`
	Kind              string  `json:"kind"`
	ContentRevisionID string  `json:"content_revision_id,omitempty"`
}
type CoverEdge struct {
	Source string `json:"source"`
	Target string `json:"target"`
}

// ActivityCounts covers every unarchived project of the account, whatever
// page or filter was asked for.
type ActivityCounts struct {
	Running int `json:"running"`
	Waiting int `json:"waiting"`
}
type ProjectPage struct {
	Items          []Project      `json:"items"`
	NextCursor     string         `json:"next_cursor"`
	ActivityCounts ActivityCounts `json:"activity_counts"`
}
type ProjectQuery struct {
	Archived bool
	Q        string
	Sort     string
	Limit    int
	Cursor   string
}

// projectCursor carries only the sort key of its own order. Names are capped
// at 200 characters before normalizing, and NFKC expands one character to at
// most 33 bytes (U+FDFA), so a name cursor stays under 6.6 KB of key, about
// 9 KB encoded. maxCursorBytes leaves room for that; there is no proxy in front
// of the server and Go accepts 1 MB of request header.
type projectCursor struct {
	Account string     `json:"account"`
	Query   string     `json:"query"`
	ID      string     `json:"id"`
	Name    string     `json:"name,omitempty"`
	Edited  *time.Time `json:"edited,omitempty"`
}

const maxCursorBytes = 16 << 10

func (q ProjectQuery) key() string {
	raw, _ := json.Marshal([]any{q.Archived, q.Q, q.Sort})
	return string(raw)
}
func decodeProjectCursor(raw, account, key, sort string) (projectCursor, error) {
	if raw == "" {
		return projectCursor{}, nil
	}
	if len(raw) > maxCursorBytes {
		return projectCursor{}, creativeops.ErrValidation
	}
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return projectCursor{}, creativeops.ErrValidation
	}
	var c projectCursor
	if err := creativeops.Decode(data, &c); err != nil || c.Account != account || c.Query != key || c.ID == "" {
		return projectCursor{}, creativeops.ErrValidation
	}
	// The query key binds the sort, so the field it needs must be there.
	if (sort == SortName) == (c.Edited != nil) {
		return projectCursor{}, creativeops.ErrValidation
	}
	return c, nil
}
func encodeProjectCursor(account, key, sort string, last Project) (string, error) {
	c := projectCursor{Account: account, Query: key, ID: last.ID}
	if sort == SortName {
		c.Name = last.normalizedName
	} else {
		c.Edited = &last.EditedAt
	}
	data, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func ListProjects(ctx context.Context, scope store.AccountScope, q ProjectQuery) (ProjectPage, error) {
	if q.Sort == "" {
		q.Sort = SortEditedDesc
	}
	if q.Limit < 1 || q.Limit > 100 || utf8.RuneCountInString(q.Q) > maxQuery ||
		(q.Sort != SortEditedDesc && q.Sort != SortEditedAsc && q.Sort != SortName) {
		return ProjectPage{}, creativeops.ErrValidation
	}
	key := q.key()
	cursor, err := decodeProjectCursor(q.Cursor, scope.AccountID(), key, q.Sort)
	if err != nil {
		return ProjectPage{}, err
	}
	result := ProjectPage{Items: []Project{}}
	// A read snapshot, not a write transaction: the account's creative write
	// lock would make canvas writes and run entries wait for the whole list.
	err = scope.WithReadSnapshot(ctx, func(tx store.ReadTxAccountScope) error {
		if err := tx.RequireCreativeRead(ctx); err != nil {
			return err
		}
		args := []any{q.Archived}
		bind := func(v any) string {
			args = append(args, v)
			return "$" + strconv.Itoa(len(args)+1)
		}
		cond := "(archived_at IS NOT NULL)=$2"
		if q.Q != "" {
			// The term goes through the generated column's own expression, then
			// LIKE metacharacters are escaped: NFKC may turn a full-width ％
			// into %, so escaping has to come after normalizing.
			term := "btrim(lower(normalize(" + bind(q.Q) + "::text,NFKC)))"
			cond += ` AND normalized_name LIKE '%'||replace(replace(replace(` + term + `,'\','\\'),'%','\%'),'_','\_')||'%' ESCAPE '\'`
		}
		order := []store.OrderBy{{Column: "edited_at", Desc: true}, {Column: "id", Desc: true}}
		switch q.Sort {
		case SortEditedAsc:
			order = []store.OrderBy{{Column: "edited_at"}, {Column: "id"}}
		case SortName:
			order = []store.OrderBy{{Column: "normalized_name"}, {Column: "id"}}
		}
		if q.Cursor != "" {
			switch q.Sort {
			case SortEditedDesc:
				cond += " AND (edited_at,id)<(" + bind(*cursor.Edited) + "," + bind(cursor.ID) + ")"
			case SortEditedAsc:
				cond += " AND (edited_at,id)>(" + bind(*cursor.Edited) + "," + bind(cursor.ID) + ")"
			case SortName:
				cond += " AND (normalized_name,id)>(" + bind(cursor.Name) + "::text COLLATE \"C\"," + bind(cursor.ID) + ")"
			}
		}
		rows, err := tx.QueryPage(ctx, "creative_projects", "id,name,revision,updated_at,edited_at,normalized_name", cond, order, q.Limit+1, 0, args...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var p Project
			var revision int64
			if err := rows.Scan(&p.ID, &p.Name, &revision, &p.UpdatedAt, &p.EditedAt, &p.normalizedName); err != nil {
				rows.Close()
				return err
			}
			p.Revision = creativeops.Revision(revision)
			p.Archived = q.Archived
			p.Cover = ProjectCover{Nodes: []CoverNode{}, Edges: []CoverEdge{}}
			result.Items = append(result.Items, p)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if len(result.Items) > q.Limit {
			result.Items = result.Items[:q.Limit]
			last := result.Items[q.Limit-1]
			result.NextCursor, err = encodeProjectCursor(scope.AccountID(), key, q.Sort, last)
			if err != nil {
				return err
			}
		}
		if err := attachProjectCanvases(ctx, tx, result.Items); err != nil {
			return err
		}
		result.ActivityCounts, err = readActivityCounts(ctx, tx)
		return err
	})
	if err != nil {
		return ProjectPage{}, err
	}
	return result, nil
}

// attachProjectCanvases fills default canvas, status and cover for one page.
// Every query is bounded by the page size, not by canvas size.
func attachProjectCanvases(ctx context.Context, tx store.ReadTxAccountScope, items []Project) error {
	if len(items) == 0 {
		return nil
	}
	byID := make(map[string]*Project, len(items))
	ids := make([]string, 0, len(items))
	for i := range items {
		byID[items[i].ID] = &items[i]
		ids = append(ids, items[i].ID)
	}
	rows, err := tx.Query(ctx, "creative_canvases", "id,project_id,is_default", "project_id=ANY($2::text[])", ids)
	if err != nil {
		return err
	}
	owner := map[string]*Project{}
	canvases := []string{}
	for rows.Next() {
		var id, project string
		var isDefault bool
		if err := rows.Scan(&id, &project, &isDefault); err != nil {
			rows.Close()
			return err
		}
		p := byID[project]
		owner[id] = p
		canvases = append(canvases, id)
		if isDefault {
			p.DefaultCanvasID = id
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, p := range items {
		if p.DefaultCanvasID == "" {
			return ErrNotFound
		}
	}
	mark := func(table, states string, set func(*Project)) error {
		rows, err := tx.Query(ctx, table, "canvas_id", "canvas_id=ANY($2::text[]) AND state IN ("+states+")", canvases)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var canvas string
			if err := rows.Scan(&canvas); err != nil {
				return err
			}
			if p := owner[canvas]; p != nil {
				set(p)
			}
		}
		return rows.Err()
	}
	running := func(p *Project) { p.Running = true }
	if err := mark("creative_agent_runs", liveRunStates, running); err != nil {
		return err
	}
	if err := mark("creative_node_executions", liveExecutionState, running); err != nil {
		return err
	}
	if err := mark("creative_agent_runs", waitingRunStates, func(p *Project) { p.Waiting = true }); err != nil {
		return err
	}
	return attachCovers(ctx, tx, items)
}

func attachCovers(ctx context.Context, tx store.ReadTxAccountScope, items []Project) error {
	sampled := []string{}
	canvases := make([]string, 0, len(items))
	cover := map[string]*ProjectCover{}
	// Positions are stored relative to the parent group; the cover needs world
	// coordinates, so every sampled node remembers its parent.
	frames := map[string]coverFrame{}
	for i := range items {
		p := &items[i]
		canvases = append(canvases, p.DefaultCanvasID)
		cover[p.DefaultCanvasID] = &p.Cover
		// Ordered by the (account_id, canvas_id, id) index: client-made random
		// ids make this a stable sample whose cost does not grow with the canvas.
		rows, err := tx.QueryPage(ctx, "creative_nodes", "id,type_key,x,y,width,height,content_revision_id,parent_id", "canvas_id=$2", []store.OrderBy{{Column: "id"}}, coverNodes+1, 0, p.DefaultCanvasID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var n CoverNode
			var typeKey string
			var revision, parent *string
			if err := rows.Scan(&n.ID, &typeKey, &n.X, &n.Y, &n.Width, &n.Height, &revision, &parent); err != nil {
				rows.Close()
				return err
			}
			if len(p.Cover.Nodes) == coverNodes {
				p.Cover.More = true
				continue
			}
			n.Kind = strings.TrimPrefix(typeKey, "core.")
			switch {
			case revision == nil && n.Kind != "group":
				n.Kind = "empty"
			case n.Kind == "image" || n.Kind == "video":
				n.ContentRevisionID = *revision
			}
			p.Cover.Nodes = append(p.Cover.Nodes, n)
			sampled = append(sampled, n.ID)
			frames[n.ID] = coverFrame{x: n.X, y: n.Y, parent: parent}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
	}
	if len(sampled) == 0 {
		return nil
	}
	if err := readAncestors(ctx, tx, frames); err != nil {
		return err
	}
	for i := range items {
		for j := range items[i].Cover.Nodes {
			n := &items[i].Cover.Nodes[j]
			n.X, n.Y = worldPoint(frames, n.ID)
		}
	}
	rows, err := tx.Query(ctx, "creative_edges", "canvas_id,source_node_id,target_node_id", "canvas_id=ANY($2::text[]) AND source_node_id=ANY($3::text[]) AND target_node_id=ANY($3::text[])", canvases, sampled)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var canvas string
		var e CoverEdge
		if err := rows.Scan(&canvas, &e.Source, &e.Target); err != nil {
			return err
		}
		if c := cover[canvas]; c != nil && len(c.Edges) < coverEdges {
			c.Edges = append(c.Edges, e)
		}
	}
	return rows.Err()
}

type coverFrame struct {
	x, y   float64
	parent *string
}

// readAncestors adds the groups above the sample, one level per round, each
// round one query for the whole page. A canvas holds at most maxNodes nodes,
// so the walk ends within that many rounds even on a malformed chain.
func readAncestors(ctx context.Context, tx store.ReadTxAccountScope, frames map[string]coverFrame) error {
	for round := 0; round < maxNodes; round++ {
		missing := []string{}
		seen := map[string]bool{}
		for _, f := range frames {
			if f.parent != nil && !seen[*f.parent] {
				if _, ok := frames[*f.parent]; !ok {
					seen[*f.parent] = true
					missing = append(missing, *f.parent)
				}
			}
		}
		if len(missing) == 0 {
			return nil
		}
		rows, err := tx.Query(ctx, "creative_nodes", "id,x,y,parent_id", "id=ANY($2::text[])", missing)
		if err != nil {
			return err
		}
		found := 0
		for rows.Next() {
			var id string
			var f coverFrame
			if err := rows.Scan(&id, &f.x, &f.y, &f.parent); err != nil {
				rows.Close()
				return err
			}
			frames[id] = f
			found++
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		// A parent that no longer exists ends its chain where it is.
		for _, id := range missing {
			if _, ok := frames[id]; !ok {
				frames[id] = coverFrame{}
			}
		}
		if found == 0 {
			return nil
		}
	}
	return nil
}

// worldPoint sums the node's position with every ancestor's, guarding against
// a cycle the same way the editor's worldPoint does.
func worldPoint(frames map[string]coverFrame, id string) (float64, float64) {
	f := frames[id]
	x, y := f.x, f.y
	seen := map[string]bool{id: true}
	for parent := f.parent; parent != nil && !seen[*parent]; {
		seen[*parent] = true
		p := frames[*parent]
		x, y = x+p.x, y+p.y
		parent = p.parent
	}
	return x, y
}

// readActivityCounts counts unarchived projects with running and with waiting
// work. A project may be counted on both sides.
func readActivityCounts(ctx context.Context, tx store.ReadTxAccountScope) (ActivityCounts, error) {
	var counts ActivityCounts
	live := "EXISTS(SELECT 1 FROM creative_canvases c WHERE c.account_id=$1 AND c.project_id=creative_projects.id AND (" +
		"EXISTS(SELECT 1 FROM creative_agent_runs r WHERE r.account_id=$1 AND r.canvas_id=c.id AND r.state IN (" + liveRunStates + ")) OR " +
		"EXISTS(SELECT 1 FROM creative_node_executions e WHERE e.account_id=$1 AND e.canvas_id=c.id AND e.state IN (" + liveExecutionState + "))))"
	waiting := "EXISTS(SELECT 1 FROM creative_canvases c WHERE c.account_id=$1 AND c.project_id=creative_projects.id AND " +
		"EXISTS(SELECT 1 FROM creative_agent_runs r WHERE r.account_id=$1 AND r.canvas_id=c.id AND r.state IN (" + waitingRunStates + ")))"
	running, err := tx.Count(ctx, "creative_projects", "archived_at IS NULL AND "+live)
	if err != nil {
		return counts, err
	}
	waited, err := tx.Count(ctx, "creative_projects", "archived_at IS NULL AND "+waiting)
	if err != nil {
		return counts, err
	}
	return ActivityCounts{Running: int(running), Waiting: int(waited)}, nil
}

// LockProjectForWorkInTx is the first lock of every entry that puts a run back
// into an active state: it takes the project row of the canvas and refuses an
// archived project. Archiving takes the same row before it checks for live
// work, so one of the two always sees the other. Lock order: project row, then
// the agent slot, the gateway group and the run.
func LockProjectForWorkInTx(ctx context.Context, tx store.TxAccountScope, canvasID string) error {
	var project string
	if err := tx.QueryRow(ctx, "creative_canvases", "project_id", "id=$2", canvasID).Scan(&project); err != nil {
		return notFound(err)
	}
	var archived *time.Time
	if err := tx.QueryRowForUpdate(ctx, "creative_projects", "archived_at", "id=$2", project).Scan(&archived); err != nil {
		return notFound(err)
	}
	if archived != nil {
		return ErrArchived
	}
	return nil
}

// projectHasLiveWork reports whether any canvas of the project has a run or
// generation that is queued, running or reconciling. The caller holds the
// project row lock, which every entry that activates work also takes.
func projectHasLiveWork(ctx context.Context, tx store.TxAccountScope, projectID string) (bool, error) {
	canvases := "SELECT c.id FROM creative_canvases c WHERE c.account_id=$1 AND c.project_id=$2"
	run, err := tx.Exists(ctx, "creative_agent_runs", "canvas_id IN ("+canvases+") AND state IN ("+liveRunStates+")", projectID)
	if err != nil || run {
		return run, err
	}
	return tx.Exists(ctx, "creative_node_executions", "canvas_id IN ("+canvases+") AND state IN ("+liveExecutionState+")", projectID)
}
