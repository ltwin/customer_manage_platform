package creativemedia

import (
	"encoding/json"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/versionedfs"
)

// AssetTarget describes the library entry a publication should create.
type AssetTarget struct {
	Title       string                   `json:"title"`
	Description string                   `json:"description,omitempty"`
	GroupIDs    []string                 `json:"group_ids,omitempty"`
	TagIDs      []string                 `json:"tag_ids,omitempty"`
	NewTags     []creativelibrary.NewTag `json:"new_tags,omitempty"`
	Favorite    bool                     `json:"is_favorite,omitempty"`
}

// NodeTarget binds the publication to an existing, still-unchanged node.
type NodeTarget struct {
	CanvasID             string               `json:"canvas_id"`
	NodeID               string               `json:"node_id"`
	ExpectedDataRevision creativeops.Revision `json:"expected_data_revision"`
}

// Target is a strict union; exactly one branch must be present.
type Target struct {
	Kind  string       `json:"kind"`
	Asset *AssetTarget `json:"asset,omitempty"`
	Node  *NodeTarget  `json:"node,omitempty"`
}

type CreateUploadInput struct {
	FileName string                                 `json:"file_name"`
	Kind     string                                 `json:"kind"`
	Mime     string                                 `json:"mime"`
	Size     int64                                  `json:"size"`
	Rights   creativecontent.RightsDeclarationInput `json:"rights"`
	Target   Target                                 `json:"target"`
}
type CompleteUploadInput struct {
	UploadID         string               `json:"upload_id"`
	ExpectedRevision creativeops.Revision `json:"expected_revision"`
	Parts            []ReportedPart       `json:"parts"`
}
type ReportedPart struct {
	Number int    `json:"part_number"`
	ETag   string `json:"etag"`
}
type CancelUploadInput struct {
	UploadID         string               `json:"upload_id"`
	ExpectedRevision creativeops.Revision `json:"expected_revision"`
}
type AuthorizePartsInput struct {
	PartNumbers []int `json:"part_numbers"`
}
type CandidateInput struct {
	CandidateID       string               `json:"candidate_id"`
	CandidateRevision creativeops.Revision `json:"candidate_revision"`
	Target            *Target              `json:"target,omitempty"`
}

// Publication is the immutable result of one publish transaction.
type Publication struct {
	Kind     string               `json:"kind"`
	ID       string               `json:"id"`
	Revision creativeops.Revision `json:"revision"`
}

// Binding reports what happened to the intended target, independently of the
// upload having finished.
type Binding struct {
	Status      string     `json:"status"`
	TargetKind  *string    `json:"target_kind"`
	TargetID    *string    `json:"target_id"`
	CandidateID *string    `json:"candidate_id"`
	ExpiresAt   *time.Time `json:"expires_at"`
}

type UploadView struct {
	ID                string               `json:"id"`
	State             string               `json:"state"`
	IOPhase           string               `json:"io_phase"`
	Revision          creativeops.Revision `json:"revision"`
	Kind              string               `json:"kind"`
	Mime              string               `json:"mime"`
	Size              int64                `json:"size"`
	FileName          string               `json:"file_name"`
	PartSize          int64                `json:"part_size"`
	PartCount         int                  `json:"part_count"`
	UploadedParts     []int                `json:"uploaded_parts"`
	TargetKind        string               `json:"target_kind"`
	ExpiresAt         time.Time            `json:"expires_at"`
	ErrorCode         string               `json:"error_code"`
	Publication       *Publication         `json:"publication"`
	Binding           *Binding             `json:"binding"`
	ContentRevisionID *string              `json:"content_revision_id"`
	CreatedAt         time.Time            `json:"created_at"`
}
type AuthorizedParts struct {
	UploadID string                          `json:"upload_id"`
	Parts    []versionedfs.PartAuthorization `json:"parts"`
}
type Candidate struct {
	ID                string               `json:"id"`
	UploadID          string               `json:"upload_id"`
	State             string               `json:"state"`
	Revision          creativeops.Revision `json:"revision"`
	Reason            string               `json:"reason"`
	Kind              string               `json:"kind"`
	FileName          string               `json:"file_name"`
	ContentRevisionID *string              `json:"content_revision_id"`
	Target            json.RawMessage      `json:"target"`
	ExpiresAt         time.Time            `json:"expires_at"`
	Adopted           *Publication         `json:"adopted"`
	CreatedAt         time.Time            `json:"created_at"`
}
type CandidatePage struct {
	Items []Candidate `json:"items"`
}
type Capabilities struct {
	SchemaVersion int      `json:"schema_version"`
	Formats       []Format `json:"formats"`
	ImageMaxBytes int64    `json:"image_max_bytes"`
	AVMaxBytes    int64    `json:"av_max_bytes"`
	PartSize      int64    `json:"part_size"`
	BatchLimit    int      `json:"batch_limit"`
}
type TicketInput struct {
	ContentRevisionID string `json:"content_revision_id"`
	Role              string `json:"role"`
	Purpose           string `json:"purpose"`
	FileName          string `json:"file_name,omitempty"`
}
type Ticket struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
	Mime      string    `json:"mime"`
	ByteSize  int64     `json:"byte_size"`
}

// upload is the persisted session row.
type upload struct {
	ID, DeclarationID, PublishOperation, State, IOPhase, Kind, Mime, Name, StagingKey, TargetKind, ErrorCode string
	StagingVersion, MultipartID, BlobID, PublishedBlob, ResultKind, ResultID                                 *string
	Epoch, Revision, Size, Reserved                                                                          int64
	ResultRevision                                                                                           *int64
	Lease, HandedOff, QuotaSettled                                                                           *time.Time
	ExpiresAt, CreatedAt                                                                                     time.Time
	Target                                                                                                   json.RawMessage
}

const uploadColumns = "id,rights_declaration_id,publish_operation_id,state,io_phase,execution_epoch,lease_until,revision,declared_kind,declared_mime,declared_size,reserved_bytes,quota_settled_at,original_name,staging_key,staging_version,multipart_id,expires_at,target_kind,target_snapshot,blob_id,handed_off_at,published_blob_id_snapshot,publication_result_kind,publication_result_id,publication_result_revision,error_code,created_at"
