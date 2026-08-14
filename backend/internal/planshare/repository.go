package planshare

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const (
	generationColumns = "id, plan_id, view_level, generation, selector, secret_commitment, fingerprint, " +
		"eligibility_link_epoch_id, state, expires_at, issued_at, rotated_at, revoked_at, invalidated_at, " +
		"first_opened_at, revision"
	offerColumns                = "id, plan_id, assignment_kind, content, state, revision, created_at, closed_at"
	assignmentManagementColumns = "id, plan_id, assignment_kind, readiness_item_id, offer_id, content_snapshot, " +
		"claimed_by_display_name, preparation_lead_days_snapshot, lead_rule_version, status, revision, " +
		"claimed_at, revoked_at, revoked_by"
	activeUnique = "share_generations_active_plan_view_idx"
)

type rowScope interface {
	QueryRow(context.Context, string, string, string, ...any) store.Row
	QueryRowForUpdate(context.Context, string, string, string, ...any) store.Row
	QueryPage(context.Context, string, string, string, []store.OrderBy, int, int, ...any) (store.Rows, error)
	ScalarAggregate(context.Context, string, string, string, string, ...any) store.Row
}

type writeScope interface {
	rowScope
	Insert(context.Context, string, []string, ...any) error
	Update(context.Context, string, string, string, ...any) (int64, error)
}

type PostgresRepository struct{}

func NewPostgresRepository() PostgresRepository { return PostgresRepository{} }

func (PostgresRepository) LoadPlanMeta(
	ctx context.Context,
	scope rowScope,
	planID string,
	forUpdate bool,
) (planIDOut string, revision int64, archived bool, err error) {
	var row store.Row
	if forUpdate {
		row = scope.QueryRowForUpdate(ctx, "shoot_plans", "id, revision, archived_at", "id = $2", planID)
	} else {
		row = scope.QueryRow(ctx, "shoot_plans", "id, revision, archived_at", "id = $2", planID)
	}
	var archivedAt sql.NullTime
	err = row.Scan(&planIDOut, &revision, &archivedAt)
	if errors.Is(err, store.ErrNoRows) {
		return "", 0, false, ErrNotFound
	}
	if err != nil {
		return "", 0, false, err
	}
	return planIDOut, revision, archivedAt.Valid, nil
}

func (PostgresRepository) LoadExecutionWindow(
	ctx context.Context,
	scope rowScope,
	planID string,
) (*ExecutionWindowHint, error) {
	var endsAt time.Time
	var revision int64
	err := scope.QueryRow(ctx, "shoot_plan_execution_windows", "ends_at, revision", "plan_id = $2", planID).
		Scan(&endsAt, &revision)
	if errors.Is(err, store.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &ExecutionWindowHint{EndsAt: endsAt.UTC(), Revision: revision}, nil
}

func (PostgresRepository) FindActiveByView(
	ctx context.Context,
	scope rowScope,
	planID string,
	view ViewLevel,
	forUpdate bool,
) (*ShareGeneration, error) {
	var row store.Row
	cond := "plan_id = $2 AND view_level = $3 AND state = 'active'"
	if forUpdate {
		row = scope.QueryRowForUpdate(ctx, "share_generations", generationColumns, cond, planID, string(view))
	} else {
		row = scope.QueryRow(ctx, "share_generations", generationColumns, cond, planID, string(view))
	}
	gen, err := scanGeneration(row)
	if errors.Is(err, store.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &gen, nil
}

func (PostgresRepository) FindByID(
	ctx context.Context,
	scope rowScope,
	planID, shareID string,
	forUpdate bool,
) (ShareGeneration, error) {
	var row store.Row
	cond := "plan_id = $2 AND id = $3"
	if forUpdate {
		row = scope.QueryRowForUpdate(ctx, "share_generations", generationColumns, cond, planID, shareID)
	} else {
		row = scope.QueryRow(ctx, "share_generations", generationColumns, cond, planID, shareID)
	}
	gen, err := scanGeneration(row)
	if errors.Is(err, store.ErrNoRows) {
		return ShareGeneration{}, ErrNotFound
	}
	return gen, err
}

func (PostgresRepository) LatestByView(
	ctx context.Context,
	scope rowScope,
	planID string,
	view ViewLevel,
) (*ShareGeneration, error) {
	if active, err := (PostgresRepository{}).FindActiveByView(ctx, scope, planID, view, false); err != nil {
		return nil, err
	} else if active != nil {
		return active, nil
	}
	rows, err := scope.QueryPage(
		ctx,
		"share_generations",
		generationColumns,
		"plan_id = $2 AND view_level = $3",
		[]store.OrderBy{{Column: "generation", Desc: true}},
		1,
		0,
		planID,
		string(view),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, nil
	}
	gen, err := scanGeneration(rows)
	if err != nil {
		return nil, err
	}
	return &gen, nil
}

func (PostgresRepository) NextGenerationNumber(
	ctx context.Context,
	scope rowScope,
	planID string,
	view ViewLevel,
) (int64, error) {
	var max sql.NullInt64
	err := scope.ScalarAggregate(
		ctx,
		"share_generations",
		store.AggregateMax,
		"generation",
		"plan_id = $2 AND view_level = $3",
		planID,
		string(view),
	).Scan(&max)
	if err != nil {
		return 0, err
	}
	if !max.Valid {
		return 1, nil
	}
	return max.Int64 + 1, nil
}

func (PostgresRepository) InsertGeneration(ctx context.Context, tx writeScope, gen ShareGeneration) error {
	err := tx.Insert(ctx, "share_generations",
		[]string{
			"id", "plan_id", "view_level", "generation", "selector", "secret_commitment", "fingerprint",
			"eligibility_link_epoch_id", "state", "expires_at", "issued_at", "revision",
		},
		gen.ID,
		gen.PlanID,
		string(gen.ViewLevel),
		gen.Generation,
		gen.Selector,
		gen.SecretCommitment[:],
		gen.Fingerprint,
		nullableString(gen.EligibilityLinkEpochID),
		string(gen.State),
		gen.ExpiresAt.UTC(),
		gen.IssuedAt.UTC(),
		gen.Revision,
	)
	if store.IsUniqueViolation(err, activeUnique) {
		return ErrShareGenerationExists
	}
	return err
}

func (PostgresRepository) MarkRotated(
	ctx context.Context,
	tx writeScope,
	planID, shareID string,
	expectedRevision int64,
	rotatedAt time.Time,
) error {
	n, err := tx.Update(
		ctx,
		"share_generations",
		"state = $2, rotated_at = $3, revision = revision + 1",
		"plan_id = $4 AND id = $5 AND state = 'active' AND revision = $6",
		string(GenerationStateRotated),
		rotatedAt.UTC(),
		planID,
		shareID,
		expectedRevision,
	)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrShareStale
	}
	return nil
}

func (PostgresRepository) MarkRevoked(
	ctx context.Context,
	tx writeScope,
	planID, shareID string,
	expectedRevision int64,
	revokedAt time.Time,
) (ShareGeneration, error) {
	n, err := tx.Update(
		ctx,
		"share_generations",
		"state = $2, revoked_at = $3, revision = revision + 1",
		"plan_id = $4 AND id = $5 AND state = 'active' AND revision = $6",
		string(GenerationStateRevoked),
		revokedAt.UTC(),
		planID,
		shareID,
		expectedRevision,
	)
	if err != nil {
		return ShareGeneration{}, err
	}
	if n == 0 {
		return ShareGeneration{}, ErrShareStale
	}
	return (PostgresRepository{}).FindByID(ctx, tx, planID, shareID, false)
}

func (PostgresRepository) ListOffersPage(
	ctx context.Context,
	scope rowScope,
	planID string,
	limit int,
	cursorCreatedAt *time.Time,
	cursorID string,
) ([]OnSiteOfferProjectionV1, *string, error) {
	if limit < 1 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	cond := "plan_id = $2"
	args := []any{planID}
	if cursorCreatedAt != nil && cursorID != "" {
		cond += " AND (created_at < $3 OR (created_at = $3 AND id < $4))"
		args = append(args, cursorCreatedAt.UTC(), cursorID)
	}
	rows, err := scope.QueryPage(
		ctx,
		"share_assignment_offers",
		offerColumns,
		cond,
		[]store.OrderBy{{Column: "created_at", Desc: true}, {Column: "id", Desc: true}},
		limit+1,
		0,
		args...,
	)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	items := make([]OnSiteOfferProjectionV1, 0, limit)
	for rows.Next() {
		item, err := scanOffer(rows)
		if err != nil {
			return nil, nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	var next *string
	if len(items) > limit {
		last := items[limit-1]
		token := encodeOfferCursor(last.CreatedAt, last.OfferID)
		next = &token
		items = items[:limit]
	}
	if err := (PostgresRepository{}).attachActiveOfferAssignments(ctx, scope, planID, items); err != nil {
		return nil, nil, err
	}
	return items, next, nil
}

func (PostgresRepository) ListAssignmentsPage(
	ctx context.Context,
	scope rowScope,
	planID string,
	limit int,
	cursorClaimedAt *time.Time,
	cursorID string,
) ([]AssignmentManagementItemV1, *string, error) {
	if limit < 1 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	cond := "plan_id = $2"
	args := []any{planID}
	if cursorClaimedAt != nil && cursorID != "" {
		cond += " AND (claimed_at < $3 OR (claimed_at = $3 AND id < $4))"
		args = append(args, cursorClaimedAt.UTC(), cursorID)
	}
	rows, err := scope.QueryPage(
		ctx,
		"share_assignments",
		assignmentManagementColumns,
		cond,
		[]store.OrderBy{{Column: "claimed_at", Desc: true}, {Column: "id", Desc: true}},
		limit+1,
		0,
		args...,
	)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	items := make([]AssignmentManagementItemV1, 0, limit)
	for rows.Next() {
		item, err := scanAssignmentManagementItem(rows)
		if err != nil {
			return nil, nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	var next *string
	if len(items) > limit {
		last := items[limit-1]
		token := encodeAssignmentCursor(last.ClaimedAt, last.AssignmentID)
		next = &token
		items = items[:limit]
	}
	return items, next, nil
}

func (PostgresRepository) ListFeedbackPage(
	ctx context.Context,
	scope rowScope,
	planID string,
	limit int,
	cursorCreatedAt *time.Time,
	cursorID string,
) ([]FeedbackManagementItemV1, *string, error) {
	if limit < 1 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	cond := "plan_id = $2"
	args := []any{planID}
	if cursorCreatedAt != nil && cursorID != "" {
		cond += " AND (created_at < $3 OR (created_at = $3 AND id < $4))"
		args = append(args, cursorCreatedAt.UTC(), cursorID)
	}
	rows, err := scope.QueryPage(
		ctx,
		"share_feedbacks",
		feedbackColumns,
		cond,
		[]store.OrderBy{{Column: "created_at", Desc: true}, {Column: "id", Desc: true}},
		limit+1,
		0,
		args...,
	)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	items := make([]FeedbackManagementItemV1, 0, limit)
	for rows.Next() {
		row, err := scanFeedbackRow(rows)
		if err != nil {
			return nil, nil, err
		}
		items = append(items, feedbackManagementItem(row))
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	var next *string
	if len(items) > limit {
		last := items[limit-1]
		token := encodeFeedbackCursor(last.CreatedAt, last.FeedbackID)
		next = &token
		items = items[:limit]
	}
	return items, next, nil
}

func feedbackManagementItem(row ShareFeedbackRow) FeedbackManagementItemV1 {
	item := FeedbackManagementItemV1{
		FeedbackID:        row.ID,
		AuthorDisplayName: row.AuthorDisplayName,
		Content:           row.Content,
		Disposition:       row.Disposition,
		Revision:          row.Revision,
		CreatedAt:         row.CreatedAt,
		DispositionAt:     row.DispositionAt,
		DeepLinkTarget:    deepLinkFromRow(row),
	}
	switch row.TargetKind {
	case FeedbackTargetPlan:
		item.Target = FeedbackTargetV1{Kind: FeedbackTargetPlan}
	case FeedbackTargetShot:
		item.Target = FeedbackTargetV1{Kind: FeedbackTargetShot, ShotID: row.TargetID}
	}
	return item
}

func encodeFeedbackCursor(createdAt time.Time, id string) string {
	raw := createdAt.UTC().Format(time.RFC3339Nano) + "|" + id
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeFeedbackCursor(cursor string) (*time.Time, string, error) {
	if cursor == "" {
		return nil, "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return nil, "", validationError("invalid feedback cursor")
	}
	createdRaw, id, ok := strings.Cut(string(raw), "|")
	if !ok || createdRaw == "" || id == "" {
		return nil, "", validationError("invalid feedback cursor")
	}
	createdAt, err := time.Parse(time.RFC3339Nano, createdRaw)
	if err != nil {
		return nil, "", validationError("invalid feedback cursor")
	}
	at := createdAt.UTC()
	return &at, id, nil
}

func scanGeneration(row interface{ Scan(...any) error }) (ShareGeneration, error) {
	var gen ShareGeneration
	var view, state string
	var commitment []byte
	var epoch sql.NullString
	var rotatedAt, revokedAt, invalidatedAt, firstOpenedAt sql.NullTime
	err := row.Scan(
		&gen.ID,
		&gen.PlanID,
		&view,
		&gen.Generation,
		&gen.Selector,
		&commitment,
		&gen.Fingerprint,
		&epoch,
		&state,
		&gen.ExpiresAt,
		&gen.IssuedAt,
		&rotatedAt,
		&revokedAt,
		&invalidatedAt,
		&firstOpenedAt,
		&gen.Revision,
	)
	if err != nil {
		return ShareGeneration{}, err
	}
	if len(commitment) != shareSecretByteLen {
		return ShareGeneration{}, fmt.Errorf("invalid secret_commitment length %d", len(commitment))
	}
	copy(gen.SecretCommitment[:], commitment)
	gen.ViewLevel = ViewLevel(view)
	gen.State = GenerationState(state)
	gen.ExpiresAt = gen.ExpiresAt.UTC()
	gen.IssuedAt = gen.IssuedAt.UTC()
	if epoch.Valid {
		value := epoch.String
		gen.EligibilityLinkEpochID = &value
	}
	gen.RotatedAt = nullTimePtr(rotatedAt)
	gen.RevokedAt = nullTimePtr(revokedAt)
	gen.InvalidatedAt = nullTimePtr(invalidatedAt)
	gen.FirstOpenedAt = nullTimePtr(firstOpenedAt)
	return gen, nil
}

func scanOffer(row interface{ Scan(...any) error }) (OnSiteOfferProjectionV1, error) {
	var item OnSiteOfferProjectionV1
	var planID string
	var closedAt sql.NullTime
	err := row.Scan(
		&item.OfferID,
		&planID,
		&item.AssignmentKind,
		&item.Content,
		&item.State,
		&item.Revision,
		&item.CreatedAt,
		&closedAt,
	)
	if err != nil {
		return OnSiteOfferProjectionV1{}, err
	}
	_ = planID
	item.CreatedAt = item.CreatedAt.UTC()
	item.ClosedAt = nullTimePtr(closedAt)
	return item, nil
}

func (r PostgresRepository) attachActiveOfferAssignments(
	ctx context.Context,
	scope rowScope,
	planID string,
	items []OnSiteOfferProjectionV1,
) error {
	for i := range items {
		var assignmentID string
		err := scope.QueryRow(ctx, "share_assignments", "id",
			"plan_id = $2 AND offer_id = $3 AND status = 'active'",
			planID, items[i].OfferID,
		).Scan(&assignmentID)
		if errors.Is(err, store.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		id := assignmentID
		items[i].ActiveAssignmentID = &id
	}
	return nil
}

func scanAssignmentManagementItem(row interface{ Scan(...any) error }) (AssignmentManagementItemV1, error) {
	var item AssignmentManagementItemV1
	var planID, kind, status string
	var readinessID, offerID, leadRule, revokedBy sql.NullString
	var leadDays sql.NullInt64
	var revokedAt sql.NullTime
	err := row.Scan(
		&item.AssignmentID,
		&planID,
		&kind,
		&readinessID,
		&offerID,
		&item.ContentSnapshot,
		&item.ClaimedByDisplayName,
		&leadDays,
		&leadRule,
		&status,
		&item.Revision,
		&item.ClaimedAt,
		&revokedAt,
		&revokedBy,
	)
	if err != nil {
		return AssignmentManagementItemV1{}, err
	}
	_ = planID
	item.AssignmentKind = AssignmentKind(kind)
	item.Status = AssignmentStatus(status)
	item.ClaimedAt = item.ClaimedAt.UTC()
	item.RevokedAt = nullTimePtr(revokedAt)
	if readinessID.Valid {
		value := readinessID.String
		item.Target = AssignmentTargetV1{Kind: AssignmentTargetReadiness, ReadinessItemID: &value}
		item.DeepLinkTarget = AssignmentDeepLinkTargetV1{Kind: AssignmentDeepLinkReadiness, ReadinessItemID: &value}
	}
	if offerID.Valid {
		value := offerID.String
		item.Target = AssignmentTargetV1{Kind: AssignmentTargetOnSiteSupport, OfferID: &value}
		item.DeepLinkTarget = AssignmentDeepLinkTargetV1{Kind: AssignmentDeepLinkOffer, OfferID: &value}
	}
	if leadDays.Valid {
		value := int(leadDays.Int64)
		item.PreparationLeadDaysSnapshot = &value
	}
	if leadRule.Valid {
		value := leadRule.String
		item.LeadRuleVersion = &value
	}
	if revokedBy.Valid {
		value := AssignmentRevokedBy(revokedBy.String)
		item.RevokedBy = &value
	}
	return item, nil
}

func nullableString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullTimePtr(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	t := value.Time.UTC()
	return &t
}

func encodeOfferCursor(createdAt time.Time, id string) string {
	raw := createdAt.UTC().Format(time.RFC3339Nano) + "|" + id
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeOfferCursor(cursor string) (*time.Time, string, error) {
	if cursor == "" {
		return nil, "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return nil, "", validationError("invalid offer cursor")
	}
	createdRaw, id, ok := strings.Cut(string(raw), "|")
	if !ok || createdRaw == "" || id == "" {
		return nil, "", validationError("invalid offer cursor")
	}
	createdAt, err := time.Parse(time.RFC3339Nano, createdRaw)
	if err != nil {
		return nil, "", validationError("invalid offer cursor")
	}
	return &createdAt, id, nil
}

func encodeAssignmentCursor(claimedAt time.Time, id string) string {
	raw := claimedAt.UTC().Format(time.RFC3339Nano) + "|" + id
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeAssignmentCursor(cursor string) (*time.Time, string, error) {
	if cursor == "" {
		return nil, "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return nil, "", validationError("invalid assignment cursor")
	}
	claimedRaw, id, ok := strings.Cut(string(raw), "|")
	if !ok || claimedRaw == "" || id == "" {
		return nil, "", validationError("invalid assignment cursor")
	}
	claimedAt, err := time.Parse(time.RFC3339Nano, claimedRaw)
	if err != nil {
		return nil, "", validationError("invalid assignment cursor")
	}
	return &claimedAt, id, nil
}
