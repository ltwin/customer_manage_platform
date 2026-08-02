package avatarmedia

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"
	"time"
)

const (
	FormatV1 = "customer-avatar-exact-generation-v1"
	FormatV2 = "avatar-exact-generation-v2"
)

// PointerSource 列举 subject-neutral 的 current pointer；本条仅 customer 实现接入。
type PointerSource interface {
	ListCurrent(ctx context.Context, accountID string, scope any, cursor string, limit int) ([]CurrentPointer, error)
}

// CurrentPointer 是中性 current 指针投影。
type CurrentPointer struct {
	AccountID      string
	SubjectKind    string
	SubjectID      string
	AvatarVersion  string
	AvatarObjectID string
	MediaType      string
	Size           int64
}

type ManifestV2 struct {
	Format      string            `json:"format"`
	GeneratedAt time.Time         `json:"generated_at"`
	Current     []CurrentObjectV2 `json:"current"`
	Inventory   InventorySummary  `json:"inventory"`
}

type CurrentObjectV2 struct {
	AccountID      string `json:"account_id"`
	SubjectKind    string `json:"subject_kind"`
	SubjectID      string `json:"subject_id"`
	AvatarVersion  string `json:"avatar_version"`
	AvatarObjectID string `json:"avatar_object_id"`
	Key            string `json:"key"`
	MediaType      string `json:"media_type"`
	Size           int64  `json:"size"`
	ActualSHA256   string `json:"actual_sha256"`
}

type ManifestV1 struct {
	Format      string            `json:"format"`
	GeneratedAt time.Time         `json:"generated_at"`
	Current     []CurrentObjectV1 `json:"current"`
	Inventory   InventorySummary  `json:"inventory"`
}

type CurrentObjectV1 struct {
	AccountID      string `json:"account_id"`
	CustomerID     string `json:"customer_id"`
	AvatarVersion  string `json:"avatar_version"`
	AvatarObjectID string `json:"avatar_object_id"`
	Key            string `json:"key"`
	MediaType      string `json:"media_type"`
	Size           int64  `json:"size"`
	ActualSHA256   string `json:"actual_sha256"`
}

type InventoryObject struct {
	Key          string `json:"key"`
	Size         int64  `json:"size"`
	ActualSHA256 string `json:"actual_sha256"`
}

type InventorySummary struct {
	Count        int               `json:"count"`
	ActualSHA256 string            `json:"actual_sha256"`
	Objects      []InventoryObject `json:"objects"`
}

// CustomerPointerFunc 适配既有 customer ListCurrentPointers，避免 avatarmedia 依赖 customer 包。
type CustomerPointerFunc func(ctx context.Context, accountID, cursor string, limit int) ([]CurrentPointer, error)

type AccountRef struct {
	AccountID string
}

// GenerateV2 生成 customer-only v2 manifest（拒绝 account_profile pointer）。
func GenerateV2(
	ctx context.Context,
	accounts []AccountRef,
	list CustomerPointerFunc,
	objects ObjectInventory,
	now time.Time,
) (ManifestV2, error) {
	return generateV2(ctx, accounts, list, objects, now, false)
}

// GenerateV2Mixed 生成含 customer＋account_profile 的 v2 manifest。
func GenerateV2Mixed(
	ctx context.Context,
	accounts []AccountRef,
	list CustomerPointerFunc,
	objects ObjectInventory,
	now time.Time,
) (ManifestV2, error) {
	return generateV2(ctx, accounts, list, objects, now, true)
}

func generateV2(
	ctx context.Context,
	accounts []AccountRef,
	list CustomerPointerFunc,
	objects ObjectInventory,
	now time.Time,
	allowMixed bool,
) (ManifestV2, error) {
	manifest := ManifestV2{Format: FormatV2, GeneratedAt: now.UTC(), Current: make([]CurrentObjectV2, 0)}
	inventory, err := objects.Inventory(ctx)
	if err != nil {
		return ManifestV2{}, fmt.Errorf("inventory avatar objects: %w", err)
	}
	physicalByKey := make(map[string]ObjectMeta, len(inventory))
	for _, item := range inventory {
		physicalByKey[item.Key.String()] = item.Meta
	}
	for _, account := range accounts {
		cursor := ""
		for {
			pointers, err := list(ctx, account.AccountID, cursor, 1000)
			if err != nil {
				return ManifestV2{}, fmt.Errorf("list current avatar pointers: %w", err)
			}
			for _, pointer := range pointers {
				if !allowMixed && pointer.SubjectKind != SubjectCustomer {
					return ManifestV2{}, errors.New("customer-only v2 writer rejected non-customer pointer")
				}
				if allowMixed && pointer.SubjectKind != SubjectCustomer && pointer.SubjectKind != SubjectAccountProfile {
					return ManifestV2{}, errors.New("unsupported subject_kind in mixed v2 writer")
				}
				entry, err := currentEntryV2(pointer, physicalByKey)
				if err != nil {
					return ManifestV2{}, err
				}
				manifest.Current = append(manifest.Current, entry)
			}
			if len(pointers) < 1000 {
				break
			}
			cursor = pointers[len(pointers)-1].SubjectID
		}
	}
	sort.Slice(manifest.Current, func(i, j int) bool {
		a, b := manifest.Current[i], manifest.Current[j]
		if a.AccountID != b.AccountID {
			return a.AccountID < b.AccountID
		}
		if a.SubjectKind != b.SubjectKind {
			return a.SubjectKind < b.SubjectKind
		}
		if a.SubjectID != b.SubjectID {
			return a.SubjectID < b.SubjectID
		}
		return a.Key < b.Key
	})
	if err := ensureUniqueSubjectsV2(manifest.Current); err != nil {
		return ManifestV2{}, err
	}
	manifest.Inventory = SummarizeInventory(inventory)
	return manifest, nil
}

// ProjectV1ForVerify 生成内部 v1 投影（customer-only，排除 account-profile key）。
func ProjectV1ForVerify(
	ctx context.Context,
	accounts []AccountRef,
	list CustomerPointerFunc,
	objects ObjectInventory,
	generatedAt time.Time,
) (ManifestV1, error) {
	manifest := ManifestV1{Format: FormatV1, GeneratedAt: generatedAt.UTC(), Current: make([]CurrentObjectV1, 0)}
	inventory, err := objects.Inventory(ctx)
	if err != nil {
		return ManifestV1{}, fmt.Errorf("inventory avatar objects: %w", err)
	}
	customerInventory := make([]ObjectItem, 0, len(inventory))
	physicalByKey := make(map[string]ObjectMeta, len(inventory))
	for _, item := range inventory {
		parsed, err := ParseKey(item.Key.String())
		if err != nil {
			return ManifestV1{}, err
		}
		if parsed.SubjectKind == SubjectAccountProfile {
			continue
		}
		if parsed.SubjectKind != SubjectCustomer {
			return ManifestV1{}, ErrObjectKey
		}
		customerInventory = append(customerInventory, item)
		physicalByKey[item.Key.String()] = item.Meta
	}
	for _, account := range accounts {
		cursor := ""
		for {
			pointers, err := list(ctx, account.AccountID, cursor, 1000)
			if err != nil {
				return ManifestV1{}, fmt.Errorf("list current avatar pointers: %w", err)
			}
			for _, pointer := range pointers {
				if pointer.SubjectKind != SubjectCustomer {
					continue
				}
				entry, err := currentEntryV1(pointer, physicalByKey)
				if err != nil {
					return ManifestV1{}, err
				}
				manifest.Current = append(manifest.Current, entry)
			}
			if len(pointers) < 1000 {
				break
			}
			cursor = pointers[len(pointers)-1].SubjectID
		}
	}
	sort.Slice(manifest.Current, func(i, j int) bool {
		if manifest.Current[i].AccountID == manifest.Current[j].AccountID {
			return manifest.Current[i].CustomerID < manifest.Current[j].CustomerID
		}
		return manifest.Current[i].AccountID < manifest.Current[j].AccountID
	})
	manifest.Inventory = SummarizeInventory(customerInventory)
	return manifest, nil
}

func VerifyV2(
	ctx context.Context,
	expected ManifestV2,
	accounts []AccountRef,
	list CustomerPointerFunc,
	objects ObjectInventory,
) error {
	return verifyV2(ctx, expected, accounts, list, objects, false)
}

// VerifyV2Mixed 用 mixed generate 比对期望 manifest。
func VerifyV2Mixed(
	ctx context.Context,
	expected ManifestV2,
	accounts []AccountRef,
	list CustomerPointerFunc,
	objects ObjectInventory,
) error {
	return verifyV2(ctx, expected, accounts, list, objects, true)
}

func verifyV2(
	ctx context.Context,
	expected ManifestV2,
	accounts []AccountRef,
	list CustomerPointerFunc,
	objects ObjectInventory,
	mixed bool,
) error {
	if expected.Format != FormatV2 {
		return errors.New("unsupported avatar manifest format")
	}
	var (
		actual ManifestV2
		err    error
	)
	if mixed {
		actual, err = GenerateV2Mixed(ctx, accounts, list, objects, expected.GeneratedAt)
	} else {
		actual, err = GenerateV2(ctx, accounts, list, objects, expected.GeneratedAt)
	}
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(expected.Current, actual.Current) {
		return errors.New("current exact-generation manifest mismatch")
	}
	if !reflect.DeepEqual(expected.Inventory, actual.Inventory) {
		return errors.New("physical avatar inventory manifest mismatch")
	}
	return nil
}

func VerifyV1(
	ctx context.Context,
	expected ManifestV1,
	accounts []AccountRef,
	list CustomerPointerFunc,
	objects ObjectInventory,
) error {
	if expected.Format != FormatV1 {
		return errors.New("unsupported avatar manifest format")
	}
	actual, err := ProjectV1ForVerify(ctx, accounts, list, objects, expected.GeneratedAt)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(expected.Current, actual.Current) {
		return errors.New("current exact-generation manifest mismatch")
	}
	if !reflect.DeepEqual(expected.Inventory, actual.Inventory) {
		return errors.New("physical avatar inventory manifest mismatch")
	}
	return nil
}

func SummarizeInventory(items []ObjectItem) InventorySummary {
	sort.Slice(items, func(i, j int) bool { return items[i].Key.String() < items[j].Key.String() })
	objects := make([]InventoryObject, 0, len(items))
	digest := sha256.New()
	for _, item := range items {
		checksum := strings.TrimPrefix(item.Meta.Checksum, "sha256-")
		key := item.Key.String()
		objects = append(objects, InventoryObject{Key: key, Size: item.Meta.Size, ActualSHA256: checksum})
		_, _ = fmt.Fprintf(digest, "%s\x00%d\x00%s\n", key, item.Meta.Size, checksum)
	}
	return InventorySummary{
		Count: len(objects), ActualSHA256: fmt.Sprintf("%x", digest.Sum(nil)), Objects: objects,
	}
}

func currentEntryV2(pointer CurrentPointer, physicalByKey map[string]ObjectMeta) (CurrentObjectV2, error) {
	if pointer.AvatarVersion == "" || pointer.AvatarObjectID == "" || pointer.MediaType == "" || pointer.Size == 0 {
		return CurrentObjectV2{}, errors.New("incomplete current avatar pointer")
	}
	ref := ObjectRef{AvatarVersion: pointer.AvatarVersion, AvatarObjectID: pointer.AvatarObjectID}
	var key Key
	var err error
	switch pointer.SubjectKind {
	case SubjectCustomer:
		key, err = CustomerKey(pointer.AccountID, pointer.SubjectID, ref)
	case SubjectAccountProfile:
		if pointer.SubjectID != pointer.AccountID {
			return CurrentObjectV2{}, errors.New("account_profile subject_id must equal account_id")
		}
		key, err = AccountProfileKey(pointer.AccountID, ref)
	default:
		return CurrentObjectV2{}, errors.New("unsupported subject_kind")
	}
	if err != nil {
		return CurrentObjectV2{}, errors.New("invalid current avatar object reference")
	}
	meta, ok := physicalByKey[key.String()]
	if !ok {
		return CurrentObjectV2{}, errors.New("current avatar generation missing from physical inventory")
	}
	if meta.MediaType != pointer.MediaType || meta.Size != pointer.Size || meta.Checksum != pointer.AvatarVersion {
		return CurrentObjectV2{}, errors.New("current avatar metadata mismatch")
	}
	return CurrentObjectV2{
		AccountID: pointer.AccountID, SubjectKind: pointer.SubjectKind, SubjectID: pointer.SubjectID,
		AvatarVersion: pointer.AvatarVersion, AvatarObjectID: pointer.AvatarObjectID,
		Key: key.String(), MediaType: meta.MediaType, Size: meta.Size,
		ActualSHA256: strings.TrimPrefix(meta.Checksum, "sha256-"),
	}, nil
}

func currentEntryV1(pointer CurrentPointer, physicalByKey map[string]ObjectMeta) (CurrentObjectV1, error) {
	entry, err := currentEntryV2(pointer, physicalByKey)
	if err != nil {
		return CurrentObjectV1{}, err
	}
	return CurrentObjectV1{
		AccountID: entry.AccountID, CustomerID: entry.SubjectID,
		AvatarVersion: entry.AvatarVersion, AvatarObjectID: entry.AvatarObjectID,
		Key: entry.Key, MediaType: entry.MediaType, Size: entry.Size, ActualSHA256: entry.ActualSHA256,
	}, nil
}

func ensureUniqueSubjectsV2(entries []CurrentObjectV2) error {
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		id := entry.AccountID + "\x00" + entry.SubjectKind + "\x00" + entry.SubjectID
		if _, ok := seen[id]; ok {
			return errors.New("duplicate current subject in manifest")
		}
		seen[id] = struct{}{}
	}
	return nil
}

// EncodeManifestCanonical 以 indent 写出；v2 generated_at 使用 UTC Z + RFC3339Nano 最短小数。
func EncodeManifestCanonical(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

// DecodeStrictManifest 严格解码：拒绝未知字段、重复 JSON key、尾随文档。
func DecodeStrictManifest(raw []byte) (format string, v1 ManifestV1, v2 ManifestV2, err error) {
	if err := rejectDuplicateJSONKeys(raw); err != nil {
		return "", ManifestV1{}, ManifestV2{}, err
	}
	var probe struct {
		Format      string          `json:"format"`
		GeneratedAt json.RawMessage `json:"generated_at"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&probe); err != nil {
		return "", ManifestV1{}, ManifestV2{}, fmt.Errorf("decode manifest: %w", err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return "", ManifestV1{}, ManifestV2{}, errors.New("manifest must contain exactly one JSON document")
	}
	if err := validateGeneratedAtRaw(probe.GeneratedAt); err != nil {
		return "", ManifestV1{}, ManifestV2{}, err
	}
	switch probe.Format {
	case FormatV1:
		var manifest ManifestV1
		if err := decodeExact(raw, &manifest); err != nil {
			return "", ManifestV1{}, ManifestV2{}, err
		}
		return FormatV1, manifest, ManifestV2{}, nil
	case FormatV2:
		var manifest ManifestV2
		if err := decodeExact(raw, &manifest); err != nil {
			return "", ManifestV1{}, ManifestV2{}, err
		}
		return FormatV2, ManifestV1{}, manifest, nil
	default:
		return "", ManifestV1{}, ManifestV2{}, errors.New("unsupported avatar manifest format")
	}
}

func decodeExact(raw []byte, dest any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dest); err != nil {
		return fmt.Errorf("decode manifest: %w", err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("manifest must contain exactly one JSON document")
	}
	return nil
}

func validateGeneratedAtRaw(raw json.RawMessage) error {
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return errors.New("manifest generated_at invalid")
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return errors.New("manifest generated_at invalid")
	}
	// 要求字面量可被 RFC3339Nano 解析且 UTC 重编码后与原字符串完全相同。
	canonical := parsed.UTC().Format(time.RFC3339Nano)
	if canonical != value {
		return errors.New("manifest generated_at not canonical")
	}
	return nil
}

func rejectDuplicateJSONKeys(raw []byte) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	delim, ok := tok.(json.Delim)
	if !ok || delim != '{' {
		return errors.New("manifest must be object")
	}
	return rejectDuplicateObjectKeys(dec)
}

func rejectDuplicateObjectKeys(dec *json.Decoder) error {
	seen := make(map[string]struct{})
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		key, ok := tok.(string)
		if !ok {
			return errors.New("manifest object key invalid")
		}
		if _, exists := seen[key]; exists {
			return errors.New("manifest duplicate json key")
		}
		seen[key] = struct{}{}
		if err := rejectDuplicateValue(dec); err != nil {
			return err
		}
	}
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '}' {
		return errors.New("manifest object unterminated")
	}
	return nil
}

func rejectDuplicateValue(dec *json.Decoder) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		return rejectDuplicateObjectKeys(dec)
	case '[':
		for dec.More() {
			if err := rejectDuplicateValue(dec); err != nil {
				return err
			}
		}
		end, err := dec.Token()
		if err != nil {
			return err
		}
		if d, ok := end.(json.Delim); !ok || d != ']' {
			return errors.New("manifest array unterminated")
		}
		return nil
	default:
		return errors.New("manifest delimiter invalid")
	}
}
