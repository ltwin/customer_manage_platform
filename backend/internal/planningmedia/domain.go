package planningmedia

import (
	"context"
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

var objectKeySegment = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func planningObjectKey(accountID, assetID string, generation int, rendition RenditionKind) (string, error) {
	if !objectKeySegment.MatchString(accountID) || !objectKeySegment.MatchString(assetID) || generation < 1 ||
		(rendition != RenditionOriginal && rendition != RenditionDisplay) {
		return "", fmt.Errorf("planning_media_object_key_invalid")
	}
	return path.Join("planning", accountID, "assets", assetID, fmt.Sprintf("g%d", generation), string(rendition)), nil
}

func parsePlanningObjectKey(key string) (accountID, assetID string, generation int, rendition RenditionKind, err error) {
	parts := strings.Split(key, "/")
	if len(parts) != 6 || parts[0] != "planning" || parts[2] != "assets" || !strings.HasPrefix(parts[4], "g") {
		return "", "", 0, "", fmt.Errorf("planning_media_object_key_invalid")
	}
	generation, err = strconv.Atoi(strings.TrimPrefix(parts[4], "g"))
	if err != nil {
		return "", "", 0, "", fmt.Errorf("planning_media_object_key_invalid")
	}
	rendering := RenditionKind(parts[5])
	canonical, err := planningObjectKey(parts[1], parts[3], generation, rendering)
	if err != nil || canonical != key {
		return "", "", 0, "", fmt.Errorf("planning_media_object_key_invalid")
	}
	return parts[1], parts[3], generation, rendering, nil
}

type AssetState string

const (
	AssetStaged    AssetState = "staged"
	AssetActive    AssetState = "active"
	AssetGCPending AssetState = "gc_pending"
	AssetDeleted   AssetState = "deleted"
	AssetCorrupt   AssetState = "corrupt"
)

type RenditionKind string

const (
	RenditionOriginal RenditionKind = "original"
	RenditionDisplay  RenditionKind = "display"
)

type BindingState string

const (
	BindingActive   BindingState = "active"
	BindingReleased BindingState = "released"
)

type LeaseState string

const (
	LeaseActive   LeaseState = "active"
	LeaseReleased LeaseState = "released"
	LeaseExpired  LeaseState = "expired"
)

type PlanAsset struct {
	ID                  string           `json:"id"`
	UploadContextPlanID string           `json:"upload_context_plan_id"`
	DisplayName         string           `json:"display_name"`
	DisplayChecksum     string           `json:"display_checksum,omitempty"`
	State               AssetState       `json:"state"`
	CurrentGeneration   int              `json:"current_generation"`
	Revision            int64            `json:"revision"`
	GCEligibleAt        *time.Time       `json:"gc_eligible_at,omitempty"`
	GCRuleVersion       int              `json:"gc_rule_version"`
	CreatedAt           time.Time        `json:"created_at"`
	UpdatedAt           time.Time        `json:"updated_at"`
	DeletedAt           *time.Time       `json:"deleted_at,omitempty"`
	Rights              *PlanAssetRights `json:"rights"`
	ActiveBindings      []AssetBinding   `json:"active_bindings"`
}

// PlanAssetRights：列表卡片展示用的当前代权利声明快照。
type PlanAssetRights struct {
	Generation                        int         `json:"generation"`
	SourceClass                       SourceClass `json:"source_class"`
	RightsBasis                       RightsBasis `json:"rights_basis"`
	LicenseGenerationReferenceGranted bool        `json:"license_generation_reference_granted"`
	DeclaredAt                        time.Time   `json:"declared_at"`
}
type AssetGeneration struct {
	AssetID          string            `json:"asset_id"`
	Generation       int               `json:"generation"`
	Rights           RightsDeclaration `json:"rights"`
	OriginalChecksum string            `json:"original_checksum"`
	DisplayChecksum  string            `json:"display_checksum"`
	CreatedAt        time.Time         `json:"created_at"`
}
type RightsDeclaration struct {
	ID                                string      `json:"id"`
	AssetID                           string      `json:"asset_id"`
	Generation                        int         `json:"generation"`
	SourceClass                       SourceClass `json:"source_class"`
	RightsBasis                       RightsBasis `json:"rights_basis"`
	EvidenceSummary                   string      `json:"evidence_summary,omitempty"`
	LicenseGenerationReferenceGranted bool        `json:"license_generation_reference_granted"`
	MatrixVersion                     int         `json:"matrix_version"`
	DeclaredAt                        time.Time   `json:"declared_at"`
}
type AssetRendition struct {
	AssetID    string        `json:"asset_id"`
	Generation int           `json:"generation"`
	Kind       RenditionKind `json:"kind"`
	MediaType  string        `json:"media_type"`
	ByteSize   int64         `json:"byte_size"`
	Width      int           `json:"width"`
	Height     int           `json:"height"`
	Checksum   string        `json:"checksum"`
	objectKey  string
}
type AssetBinding struct {
	ID         string       `json:"id"`
	AssetID    string       `json:"asset_id"`
	Generation int          `json:"generation"`
	HolderKind HolderKind   `json:"holder_kind"`
	HolderID   string       `json:"holder_id"`
	PlanID     string       `json:"plan_id"`
	Purpose    Purpose      `json:"purpose"`
	State      BindingState `json:"state"`
	Revision   int64        `json:"revision"`
	CreatedAt  time.Time    `json:"created_at"`
	ReleasedAt *time.Time   `json:"released_at,omitempty"`
}
type AssetLease struct {
	ID         string     `json:"id"`
	AssetID    string     `json:"asset_id"`
	Generation int        `json:"generation"`
	BindingID  string     `json:"binding_id"`
	OwnerKind  string     `json:"owner_kind"`
	OwnerID    string     `json:"owner_id"`
	Purpose    Purpose    `json:"purpose"`
	State      LeaseState `json:"state"`
	ExpiresAt  time.Time  `json:"expires_at"`
	Revision   int64      `json:"revision"`
}
type AssetAccessRef struct {
	AssetID         string `json:"asset_id"`
	Generation      int    `json:"generation"`
	DisplayChecksum string `json:"display_checksum"`
	DisplayName     string `json:"display_name"`
}
type PlanAssetPage struct {
	Items      []PlanAsset `json:"items"`
	NextCursor string      `json:"next_cursor,omitempty"`
	Done       bool        `json:"done"`
}

type HolderProof struct {
	accountSeal  string
	planID       string
	holderID     string
	kind         HolderKind
	planRevision int64
	stateSeal    string
}

func NewHolderProof(accountID, planID, holderID string, kind HolderKind, planRevision int64, state string) HolderProof {
	return HolderProof{accountSeal: accountID, planID: planID, holderID: holderID, kind: kind, planRevision: planRevision, stateSeal: state}
}

func (p HolderProof) ValidFor(accountID, planID, holderID string, kind HolderKind, revision int64) bool {
	return p.accountSeal != "" && p.accountSeal == accountID && p.planID == planID && p.holderID == holderID && p.kind == kind && p.planRevision == revision && p.stateSeal != ""
}

type HolderRequest struct {
	PlanID               string
	HolderID             string
	Kind                 HolderKind
	ExpectedPlanRevision int64
	Mutation             HolderMutation
}
type HolderAuthorizer interface {
	AuthorizeMediaHolderInScope(context.Context, store.TxAccountScope, HolderRequest) (HolderProof, error)
}
