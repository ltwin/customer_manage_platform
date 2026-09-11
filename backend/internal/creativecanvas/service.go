// Package creativecanvas owns project and canvas application commands.
package creativecanvas

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

var (
	ErrNotFound        = errors.New("creative project or canvas object not found")
	ErrArchived        = errors.New("creative project is archived")
	ErrVersionConflict = errors.New("creative canvas version conflict")
	ErrLimit           = errors.New("creative canvas node limit reached")
)

const maxNodes = 500

type CreateProjectInput struct {
	Name string `json:"name"`
}
type ProjectStateInput struct {
	ProjectID        string               `json:"project_id"`
	ExpectedRevision creativeops.Revision `json:"expected_revision"`
}
type RenameProjectInput struct {
	ProjectID        string               `json:"project_id"`
	ExpectedRevision creativeops.Revision `json:"expected_revision"`
	Name             string               `json:"name"`
}
type ProjectResult struct {
	ProjectID string               `json:"project_id"`
	CanvasID  string               `json:"default_canvas_id"`
	Revision  creativeops.Revision `json:"revision"`
}
type AddNodeInput struct {
	CanvasID                 string                          `json:"canvas_id"`
	NodeID                   string                          `json:"node_id"`
	TypeKey                  string                          `json:"type_key"`
	Title                    string                          `json:"title"`
	X                        float64                         `json:"x"`
	Y                        float64                         `json:"y"`
	ExpectedTopologyRevision creativeops.Revision            `json:"expected_topology_revision"`
	Asset                    *creativelibrary.AssetReference `json:"asset,omitempty"`
	Content                  *creativecontent.Draft          `json:"content,omitempty"`
}
type ReplaceContentInput struct {
	CanvasID                  string                  `json:"canvas_id"`
	NodeID                    string                  `json:"node_id"`
	ExpectedDataRevision      creativeops.Revision    `json:"expected_data_revision"`
	ExpectedContentRevisionID *string                 `json:"expected_content_revision_id"`
	Payload                   creativecontent.Payload `json:"payload"`
	// Rights are required for the first content of an empty node, never used to
	// upgrade a derived revision's rights.
	ContentRights *creativecontent.RightsDeclarationInput `json:"rights,omitempty"`
}
type MoveNodeInput struct {
	CanvasID                  string               `json:"canvas_id"`
	NodeID                    string               `json:"node_id"`
	ExpectedPlacementRevision creativeops.Revision `json:"expected_placement_revision"`
	X                         float64              `json:"x"`
	Y                         float64              `json:"y"`
}
type NodeResult struct {
	NodeID                 string               `json:"node_id"`
	CanvasRevision         creativeops.Revision `json:"result_revision"`
	BeforeTopologyRevision creativeops.Revision `json:"before_topology_revision"`
	TopologyRevision       creativeops.Revision `json:"result_topology_revision"`
	PlacementRevision      creativeops.Revision `json:"placement_revision"`
	DataRevision           creativeops.Revision `json:"data_revision"`
	ContentRevisionID      *string              `json:"content_revision_id"`
}
type Node struct {
	ReadOnly          bool                 `json:"-"`
	Status            NodeStatus           `json:"-"`
	Prompt            *NodePrompt          `json:"-"`
	ParentID          *string              `json:"parent_id"`
	TypeVersion       int                  `json:"type_version"`
	ZOrder            int                  `json:"z_order"`
	Intent            string               `json:"intent"`
	Config            json.RawMessage      `json:"config"`
	SelectedVersionID *string              `json:"selected_version_id"`
	DocumentID        *string              `json:"document_id"`
	StatusRevision    creativeops.Revision `json:"status_revision"`
	ActiveExecutionID *string              `json:"active_execution_id"`
	LatestExecutionID *string              `json:"latest_execution_id"`

	ID                string                    `json:"id"`
	TypeKey           string                    `json:"type_key"`
	Title             string                    `json:"title"`
	X                 float64                   `json:"x"`
	Y                 float64                   `json:"y"`
	Width             float64                   `json:"width"`
	Height            float64                   `json:"height"`
	PlacementRevision creativeops.Revision      `json:"placement_revision"`
	DataRevision      creativeops.Revision      `json:"data_revision"`
	ContentID         *string                   `json:"content_id"`
	ContentRevisionID *string                   `json:"content_revision_id"`
	Content           *creativecontent.Revision `json:"content,omitempty"`
	Unavailable       bool                      `json:"unavailable"`
}
type Canvas struct {
	ObjectStates     []ObjectResult       `json:"object_states"`
	ProjectName      string               `json:"project_name"`
	ProjectRevision  creativeops.Revision `json:"project_revision"`
	ID               string               `json:"id"`
	ProjectID        string               `json:"project_id"`
	Archived         bool                 `json:"archived"`
	Revision         creativeops.Revision `json:"revision"`
	TopologyRevision creativeops.Revision `json:"topology_revision"`
	Nodes            []Node               `json:"nodes"`
	Edges            []Edge               `json:"edges"`
	Inputs           []NodeInput          `json:"node_inputs"`
	Changes          []ChangeSummary      `json:"changes"`
}

func run[T any](ctx context.Context, scope store.AccountScope, key string, c creativeops.Command, validate func(T) error, apply func(context.Context, store.TxAccountScope, T) (creativeops.Outcome, error), wireChecks ...func(json.RawMessage) error) (creativeops.Receipt, error) {
	return (creativeops.Executor{}).Run(ctx, scope, creativeops.Operation{Key: key, Capability: "manual_write", Validate: func(raw json.RawMessage) error {
		for _, check := range wireChecks {
			if err := check(raw); err != nil {
				return err
			}
		}
		var v T
		if err := creativeops.Decode(raw, &v); err != nil {
			return err
		}
		return validate(v)
	}, Apply: func(ctx context.Context, tx store.TxAccountScope, raw json.RawMessage) (creativeops.Outcome, error) {
		var v T
		if err := creativeops.Decode(raw, &v); err != nil {
			return creativeops.Outcome{}, err
		}
		return apply(ctx, tx, v)
	}}, c)
}
func outcome(status int, kind, id string, revision creativeops.Revision, value any) (creativeops.Outcome, error) {
	body, err := json.Marshal(value)
	return creativeops.Outcome{HTTPStatus: status, ResultKind: kind, ResultID: &id, ResultRevision: &revision, Response: body}, err
}
func validName(s string) bool { return strings.TrimSpace(s) != "" && utf8.RuneCountInString(s) <= 200 }
func validPoint(x, y float64) bool {
	return !math.IsNaN(x) && !math.IsNaN(y) && math.Abs(x) <= 1e7 && math.Abs(y) <= 1e7
}
func validNodeID(id string) bool {
	return strings.HasPrefix(id, "cwnode_") && creativeops.ValidOperationID(strings.TrimPrefix(id, "cwnode_"))
}
func notFound(err error) error {
	if errors.Is(err, store.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func CreateProject(ctx context.Context, scope store.AccountScope, c creativeops.Command) (creativeops.Receipt, error) {
	return run(ctx, scope, "canvas.create_project", c, func(v CreateProjectInput) error {
		if !validName(v.Name) {
			return creativeops.ErrValidation
		}
		return nil
	}, func(ctx context.Context, tx store.TxAccountScope, v CreateProjectInput) (creativeops.Outcome, error) {
		p := ProjectResult{ProjectID: "ccpj_" + uuid.NewString(), CanvasID: "cccv_" + uuid.NewString(), Revision: 1}
		if err := tx.Insert(ctx, "creative_projects", []string{"id", "name"}, p.ProjectID, v.Name); err != nil {
			return creativeops.Outcome{}, err
		}
		if err := tx.Insert(ctx, "creative_canvases", []string{"id", "project_id", "name"}, p.CanvasID, p.ProjectID, "主画布"); err != nil {
			return creativeops.Outcome{}, err
		}
		return outcome(201, "project", p.ProjectID, p.Revision, p)
	})
}
func validateProjectState(v ProjectStateInput) error {
	if v.ProjectID == "" || v.ExpectedRevision < 1 {
		return creativeops.ErrValidation
	}
	return nil
}
func ArchiveProject(ctx context.Context, scope store.AccountScope, c creativeops.Command) (creativeops.Receipt, error) {
	return setArchived(ctx, scope, c, true)
}
func RestoreProject(ctx context.Context, scope store.AccountScope, c creativeops.Command) (creativeops.Receipt, error) {
	return setArchived(ctx, scope, c, false)
}
func setArchived(ctx context.Context, scope store.AccountScope, c creativeops.Command, archive bool) (creativeops.Receipt, error) {
	key := "canvas.restore_project"
	if archive {
		key = "canvas.archive_project"
	}
	return run(ctx, scope, key, c, validateProjectState, func(ctx context.Context, tx store.TxAccountScope, v ProjectStateInput) (creativeops.Outcome, error) {
		var revision int64
		var archived *time.Time
		if err := tx.QueryRowForUpdate(ctx, "creative_projects", "revision,archived_at", "id=$2", v.ProjectID).Scan(&revision, &archived); err != nil {
			return creativeops.Outcome{}, notFound(err)
		}
		if creativeops.Revision(revision) != v.ExpectedRevision {
			return creativeops.Outcome{}, ErrVersionConflict
		}
		if (archived != nil) != archive {
			var value *time.Time
			if archive {
				now, err := tx.CreativeNow(ctx)
				if err != nil {
					return creativeops.Outcome{}, err
				}
				value = &now
			}
			if _, err := tx.Update(ctx, "creative_projects", "archived_at=$2,revision=revision+1,updated_at=clock_timestamp()", "id=$3", value, v.ProjectID); err != nil {
				return creativeops.Outcome{}, err
			}
			revision++
		}
		return outcome(200, "project", v.ProjectID, creativeops.Revision(revision), struct {
			ProjectID string               `json:"project_id"`
			Revision  creativeops.Revision `json:"revision"`
			Archived  bool                 `json:"archived"`
		}{v.ProjectID, creativeops.Revision(revision), archive})
	})
}
func RenameProject(ctx context.Context, scope store.AccountScope, c creativeops.Command) (creativeops.Receipt, error) {
	return run(ctx, scope, "canvas.rename_project", c, func(v RenameProjectInput) error {
		if !validName(v.Name) {
			return creativeops.ErrValidation
		}
		return validateProjectState(ProjectStateInput{v.ProjectID, v.ExpectedRevision})
	}, func(ctx context.Context, tx store.TxAccountScope, v RenameProjectInput) (creativeops.Outcome, error) {
		var revision int64
		var archived *time.Time
		if err := tx.QueryRowForUpdate(ctx, "creative_projects", "revision,archived_at", "id=$2", v.ProjectID).Scan(&revision, &archived); err != nil {
			return creativeops.Outcome{}, notFound(err)
		}
		if archived != nil {
			return creativeops.Outcome{}, ErrArchived
		}
		if creativeops.Revision(revision) != v.ExpectedRevision {
			return creativeops.Outcome{}, ErrVersionConflict
		}
		if _, err := tx.Update(ctx, "creative_projects", "name=$2,revision=revision+1,updated_at=clock_timestamp()", "id=$3", v.Name, v.ProjectID); err != nil {
			return creativeops.Outcome{}, err
		}
		return outcome(200, "project", v.ProjectID, creativeops.Revision(revision+1), struct {
			ProjectID string               `json:"project_id"`
			Revision  creativeops.Revision `json:"revision"`
		}{v.ProjectID, creativeops.Revision(revision + 1)})
	})
}

func lockCanvas(ctx context.Context, tx store.TxAccountScope, id string, write bool) (Canvas, error) {
	var c Canvas
	c.ID = id
	if err := tx.QueryRow(ctx, "creative_canvases", "project_id", "id=$2", id).Scan(&c.ProjectID); err != nil {
		return Canvas{}, notFound(err)
	}
	var archived *time.Time
	var projectRevision int64
	if err := tx.QueryRowForUpdate(ctx, "creative_projects", "archived_at,name,revision", "id=$2", c.ProjectID).Scan(&archived, &c.ProjectName, &projectRevision); err != nil {
		return Canvas{}, notFound(err)
	}
	c.Archived = archived != nil
	c.ProjectRevision = creativeops.Revision(projectRevision)
	if write && c.Archived {
		return Canvas{}, ErrArchived
	}
	var revision, topology int64
	if err := tx.QueryRowForUpdate(ctx, "creative_canvases", "revision,topology_revision", "id=$2 AND project_id=$3", id, c.ProjectID).Scan(&revision, &topology); err != nil {
		return Canvas{}, notFound(err)
	}
	c.Revision = creativeops.Revision(revision)
	c.TopologyRevision = creativeops.Revision(topology)
	return c, nil
}
func advanceCanvas(ctx context.Context, tx store.TxAccountScope, c Canvas, structural bool) error {
	if structural {
		_, err := tx.Update(ctx, "creative_canvases", "revision=revision+1,topology_revision=topology_revision+1,updated_at=clock_timestamp()", "id=$2", c.ID)
		return err
	}
	_, err := tx.Update(ctx, "creative_canvases", "revision=revision+1,updated_at=clock_timestamp()", "id=$2", c.ID)
	return err
}
func GetCanvas(ctx context.Context, scope store.AccountScope, id string) (Canvas, error) {
	var c Canvas
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		// The current foundation barrier serializes this bounded snapshot with all
		// participating writers, including capability and usage changes.
		if err := tx.RequireCreativeCapability(ctx, "creative_read"); err != nil {
			return err
		}
		var err error
		c, err = lockCanvas(ctx, tx, id, false)
		if err != nil {
			return err
		}
		rows, err := tx.QueryPage(ctx, "creative_nodes", nodeColumns, "canvas_id=$2", []store.OrderBy{{Column: "id"}}, maxNodes+1, 0, id)
		if err != nil {
			return err
		}
		c.Nodes = []Node{}
		for rows.Next() {
			n, err := scanNode(rows)
			if err != nil {
				rows.Close()
				return err
			}
			c.Nodes = append(c.Nodes, n)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if len(c.Nodes) > maxNodes {
			return ErrLimit
		}
		for i := range c.Nodes {
			n := &c.Nodes[i]
			n.ReadOnly = c.Archived
			if err := fillNodeView(ctx, tx, id, n); err != nil {
				return err
			}
			if n.ContentRevisionID == nil || !knownType(n.TypeKey) {
				continue
			}
			r, err := creativecontent.RequireUsable(ctx, tx, *n.ContentRevisionID, "display")
			if errors.Is(err, creativecontent.ErrUsageDenied) {
				n.Unavailable = true
				continue
			}
			if err != nil {
				return err
			}
			if n.ContentID == nil || r.ContentID != *n.ContentID || n.TypeKey != "core."+r.Kind {
				return creativecontent.ErrMissingRoot
			}
			preview := creativecontent.Preview(r)
			n.Content = &preview
		}
		if err := fillGraphSnapshot(ctx, tx, &c); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return Canvas{}, err
	}
	return c, nil
}

func requirePosition(raw json.RawMessage) error {
	var fields map[string]json.RawMessage
	if err := creativeops.Decode(raw, &fields); err != nil {
		return err
	}
	for _, key := range []string{"x", "y"} {
		value, ok := fields[key]
		if !ok || strings.TrimSpace(string(value)) == "null" {
			return creativeops.ErrValidation
		}
	}
	return nil
}
func requireContentPointer(raw json.RawMessage) error {
	var fields map[string]json.RawMessage
	if err := creativeops.Decode(raw, &fields); err != nil {
		return err
	}
	if _, ok := fields["expected_content_revision_id"]; !ok {
		return creativeops.ErrValidation
	}
	return nil
}
