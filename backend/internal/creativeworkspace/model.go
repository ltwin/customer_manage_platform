// Package creativeworkspace 是创作空间先导（creative-workspace-redesign）的领域层。
//
// 边界（Epic DEC-3 / DEC-5 / DEC-15 / DEC-16 / DEC-17）：
//   - 卡片由账号持有，空间归属、顺序与分组只经成员关系表达；先导版一卡同一时间只属于一个空间。
//   - 拍摄项由卡片值拷贝而来，创建后与来源无任何同步；来源引用只有 available | unavailable 两态。
//   - 空间到订单 / 客户的关联是单值可空外键，对经营侧零写。
//   - 拍摄备忘没有 required / 截止 / 负责人 / 提醒，不阻断任何操作。
//   - 本包不 import 路由框架；gin.Context 不下穿（ADR-003）。
package creativeworkspace

import (
	"errors"
	"time"
)

var (
	ErrValidation         = errors.New("validation_failed")
	ErrNotFound           = errors.New("not_found")
	ErrWorkspaceArchived  = errors.New("workspace_archived")
	ErrInboxImmutable     = errors.New("inbox_immutable")
	ErrRevisionConflict   = errors.New("workspace_revision_conflict")
	ErrLinkTargetNotFound = errors.New("link_target_not_found")
	ErrCardNotInWorkspace = errors.New("card_not_in_workspace")
	ErrNoValidShootItems  = errors.New("no_valid_shoot_items")
	ErrPilotRequired      = errors.New("pilot_required")
	ErrMediaUnsupported   = errors.New("media_unsupported")
)

// ValidationError 带用户可读消息的输入错误，Unwrap 到 ErrValidation。
type ValidationError struct{ Message string }

func (e ValidationError) Error() string { return e.Message }
func (e ValidationError) Unwrap() error { return ErrValidation }

type WorkspaceKind string

const (
	KindProject WorkspaceKind = "project"
	KindInbox   WorkspaceKind = "inbox"
)

type LinkKind string

const (
	LinkOrder    LinkKind = "order"
	LinkCustomer LinkKind = "customer"
)

// WorkspaceLink 是空间到订单或客户的纯读引用（DEC-15）。
type WorkspaceLink struct {
	Kind LinkKind `json:"kind"`
	ID   string   `json:"id"`
	// Name / Sub 由读侧解析；Broken 表示目标已删除或不可见（外键 SET NULL 后不会出现，但客户归档等情况仍需显示失效）。
	Name   string `json:"name"`
	Sub    string `json:"sub,omitempty"`
	Broken bool   `json:"broken"`
}

type Workspace struct {
	ID           string         `json:"id"`
	Kind         WorkspaceKind  `json:"kind"`
	Name         string         `json:"name"`
	Link         *WorkspaceLink `json:"link,omitempty"`
	Archived     bool           `json:"archived"`
	Revision     int64          `json:"revision"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	LastOpenedAt time.Time      `json:"last_opened_at"`
	// 列表用汇总
	CardCount      int     `json:"card_count"`
	CoverAssetID   *string `json:"cover_asset_id,omitempty"`
	CoverChecksum  *string `json:"cover_checksum,omitempty"`
	ShootItemCount int     `json:"shoot_item_count"`
}

// DisplayName 未命名时回落为关联对象名（验收 19）。
func (w Workspace) DisplayName() string {
	if w.Kind == KindInbox {
		return "未归类"
	}
	if w.Name != "" {
		return w.Name
	}
	if w.Link != nil && w.Link.Name != "" {
		return w.Link.Name
	}
	return ""
}

type CardType string

const (
	CardText  CardType = "text"
	CardImage CardType = "image"
	CardLink  CardType = "link"
)

// SourceClass 与 planningmedia 权利矩阵同一枚举；先导版默认 unknown_web（最严格）。
type SourceClass string

const SourceUnknownWeb SourceClass = "unknown_web"

type Card struct {
	ID          string       `json:"id"`
	Type        CardType     `json:"type"`
	Text        string       `json:"text,omitempty"`
	URL         string       `json:"url,omitempty"`
	AssetID     string       `json:"asset_id,omitempty"`
	Checksum    string       `json:"display_checksum,omitempty"`
	Width       int          `json:"width,omitempty"`
	Height      int          `json:"height,omitempty"`
	Caption     string       `json:"caption,omitempty"`
	SourceClass *SourceClass `json:"source_class,omitempty"`
	BatchID     string       `json:"batch_id,omitempty"`
	BatchSeq    int          `json:"batch_seq,omitempty"`
	Revision    int64        `json:"revision"`
	CreatedAt   time.Time    `json:"created_at"`
	Archived    bool         `json:"archived"`
	// 成员关系（布局）
	WorkspaceID string  `json:"workspace_id"`
	GroupID     *string `json:"group_id,omitempty"`
	Position    int     `json:"position"`
}

type Group struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	Name        string    `json:"name"`
	Position    int       `json:"position"`
	CreatedAt   time.Time `json:"created_at"`
}

type ImportBatch struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	SourceKind  string    `json:"source_kind"`
	CardCount   int       `json:"card_count"`
	CreatedAt   time.Time `json:"created_at"`
}

type SourceState string

const (
	SourceAvailable   SourceState = "available"
	SourceUnavailable SourceState = "unavailable"
)

type ShootItemStatus string

const (
	ShootActive    ShootItemStatus = "active"
	ShootTombstone ShootItemStatus = "tombstone"
)

type ShootResult string

const (
	ResultDone    ShootResult = "done"
	ResultSkipped ShootResult = "skipped"
)

type ShootItem struct {
	ID                string          `json:"id"`
	WorkspaceID       string          `json:"workspace_id"`
	Title             string          `json:"title"`
	Description       string          `json:"description"`
	RefAssetID        *string         `json:"ref_asset_id,omitempty"`
	RefChecksum       *string         `json:"ref_display_checksum,omitempty"`
	SourceCardID      *string         `json:"source_card_id,omitempty"`
	SourceCardRev     *int64          `json:"source_card_rev,omitempty"`
	SourceCardIDs     []string        `json:"source_card_ids"`
	SourceWorkspaceID *string         `json:"source_workspace_id,omitempty"`
	SourceState       SourceState     `json:"source_state"`
	Position          int             `json:"position"`
	Status            ShootItemStatus `json:"status"`
	Result            *ShootResult    `json:"result,omitempty"`
	ResultNote        string          `json:"result_note"`
	ResultAt          *time.Time      `json:"result_at,omitempty"`
	Revision          int64           `json:"revision"`
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
}

type Memo struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	Text        string    `json:"text"`
	Checked     bool      `json:"checked"`
	Position    int       `json:"position"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Asset 是创作空间媒体资产（独立生命周期；只复用 planningmedia 的管线与矩阵）。
type Asset struct {
	ID                         string      `json:"id"`
	WorkspaceID                string      `json:"workspace_id"`
	SourceClass                SourceClass `json:"source_class"`
	RightsBasis                string      `json:"rights_basis"`
	GenerationReferenceGranted bool        `json:"generation_reference_granted"`
	OriginalMediaType          string      `json:"original_media_type"`
	OriginalSize               int64       `json:"original_size"`
	OriginalWidth              int         `json:"original_width"`
	OriginalHeight             int         `json:"original_height"`
	OriginalChecksum           string      `json:"original_checksum"`
	DisplayMediaType           string      `json:"display_media_type"`
	DisplaySize                int64       `json:"display_size"`
	DisplayWidth               int         `json:"display_width"`
	DisplayHeight              int         `json:"display_height"`
	DisplayChecksum            string      `json:"display_checksum"`
	CreatedAt                  time.Time   `json:"created_at"`
}

// WorkspaceDetail 是空间内页一次读取的全部内容。
type WorkspaceDetail struct {
	Workspace  Workspace   `json:"workspace"`
	Cards      []Card      `json:"cards"`
	Groups     []Group     `json:"groups"`
	ShootItems []ShootItem `json:"shoot_items"`
	Memos      []Memo      `json:"memos"`
}

// ---- 输入 ----

type CreateWorkspaceInput struct {
	Name string
}

type UpdateWorkspaceInput struct {
	ExpectedRevision int64
	Name             *string
	// Link: nil 不改；&LinkInput{Kind:""} 解除；否则设置。
	Link     *LinkInput
	Archived *bool
	Touch    bool // 记录打开
}

type LinkInput struct {
	Kind LinkKind
	ID   string
}

// ImportCardsInput 一次批量搬入：多行文本按行成卡、链接原样成链接卡；图片以已上传的 asset ID 成卡。
type ImportCardsInput struct {
	WorkspaceID string
	RawText     string   // 粘贴的原始整段文本（DEC-17 (b)），可为空
	AssetIDs    []string // 已上传到本空间的资产
	GroupID     *string
}

type ImportCardsResult struct {
	Batch ImportBatch `json:"batch"`
	Cards []Card      `json:"cards"`
}

type UpdateCardInput struct {
	Caption *string
	Text    *string
}

type ReorderInput struct {
	WorkspaceID string
	// CardIDs 是该空间全部有效卡片的新顺序；缺失的卡片按原相对顺序追加到末尾。
	CardIDs []string
}

type MoveCardInput struct {
	FromWorkspaceID string
	CardID          string
	ToWorkspaceID   string
	ToGroupID       *string
}

type CreateGroupInput struct {
	WorkspaceID string
	Name        string
	CardIDs     []string
}

type SetCardGroupInput struct {
	WorkspaceID string
	CardIDs     []string
	GroupID     *string // nil = 移出分组
}

type CreateShootItemsInput struct {
	WorkspaceID string
	CardIDs     []string
	Merge       bool // true：多卡合成一个拍摄项；false：每卡一个
}

type UpdateShootItemInput struct {
	ExpectedRevision int64
	Title            *string
	Description      *string
	Tombstone        bool
	// 现场结果：Result==nil 且 ClearResult 才撤销
	Result      *ShootResult
	ResultNote  *string
	ClearResult bool
}

type ReorderShootItemsInput struct {
	WorkspaceID string
	ItemIDs     []string
}

type CreateMemosInput struct {
	WorkspaceID string
	Text        string // 多行
}

type UpdateMemoInput struct {
	Text    *string
	Checked *bool
}

type ReorderMemosInput struct {
	WorkspaceID string
	MemoIDs     []string
}

// UploadAssetInput 由 HTTP 层解码 multipart 后填充；Bytes 已经过大小上限校验。
type UploadAssetInput struct {
	WorkspaceID       string
	DeclaredMediaType string
	Bytes             []byte
	SourceClass       SourceClass
	// RightsBasis 由 SourceClass 推导（矩阵唯一合法值），调用方不传。
}

// ---- pilot（ITEM-2） ----

type PilotState string

const (
	PilotLegacyWrite PilotState = "legacy_write"
	PilotNewWrite    PilotState = "pilot_new_write"
	PilotStopped     PilotState = "stopped"
)

type PilotAccount struct {
	CanEnroll  bool       `json:"can_enroll"`
	State      PilotState `json:"state"`
	EnrolledAt *time.Time `json:"enrolled_at,omitempty"`
	StoppedAt  *time.Time `json:"stopped_at,omitempty"`
	WindowID   *string    `json:"window_id,omitempty"`
	Revision   int64      `json:"revision"`
}

// PreflightItem 是逐类清场清单中的一行（Epic 验收 9；B1 §4）。
type PreflightItem struct {
	Key      string `json:"key"`
	Title    string `json:"title"`
	Count    int    `json:"count"`
	Blocking bool   `json:"blocking"`
	HowTo    string `json:"how_to"`
}

type PreflightResult struct {
	Eligible bool            `json:"eligible"`
	Items    []PreflightItem `json:"items"`
}
