package planningmedia

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"

	"github.com/samson/customer-manage-platform/backend/internal/platform/immutablefs"
)

type ManifestEntry struct {
	Key        string               `json:"key"`
	AssetID    string               `json:"asset_id"`
	Generation int                  `json:"generation"`
	Rendition  RenditionKind        `json:"rendition"`
	Metadata   immutablefs.Metadata `json:"metadata"`
}
type PlanningMediaManifestV1 struct {
	Version int             `json:"version"`
	Entries []ManifestEntry `json:"entries"`
	Digest  string          `json:"digest"`
}

type InventoryClassification string

const (
	InventoryMissing InventoryClassification = "missing"
	InventoryOrphan  InventoryClassification = "orphan"
	InventoryCorrupt InventoryClassification = "corrupt"
)

type InventoryFinding struct {
	Key            string
	Classification InventoryClassification
}

func BuildManifest(ctx context.Context, objects immutablefs.ObjectStore) (PlanningMediaManifestV1, error) {
	items, err := listAll(ctx, objects)
	if err != nil {
		return PlanningMediaManifestV1{}, err
	}
	entries := make([]ManifestEntry, 0, len(items))
	for _, item := range items {
		_, assetID, generation, rendition, err := parsePlanningObjectKey(item.Key)
		if err != nil {
			return PlanningMediaManifestV1{}, err
		}
		entries = append(entries, ManifestEntry{Key: item.Key, AssetID: assetID, Generation: generation, Rendition: rendition, Metadata: item.Meta})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Key < entries[j].Key })
	manifest := PlanningMediaManifestV1{Version: 1, Entries: entries}
	manifest.Digest = manifestDigest(manifest)
	return manifest, nil
}

func VerifyAndRestoreFixture(ctx context.Context, manifest PlanningMediaManifestV1, source, target immutablefs.ObjectStore) error {
	if manifest.Version != 1 {
		return fmt.Errorf("manifest_version_invalid")
	}
	if manifest.Digest == "" || manifest.Digest != manifestDigest(manifest) {
		return fmt.Errorf("manifest_digest_invalid")
	}
	targetItems, err := listAll(ctx, target)
	if err != nil {
		return err
	}
	if len(targetItems) != 0 {
		return fmt.Errorf("restore_target_not_empty")
	}
	sourceItems, err := listAll(ctx, source)
	if err != nil {
		return err
	}
	if findings := ClassifyInventory(manifest.Entries, sourceItems); len(findings) != 0 {
		return fmt.Errorf("restore_source_inventory_mismatch")
	}
	created := make([]string, 0, len(manifest.Entries))
	previousKey := ""
	for _, entry := range manifest.Entries {
		_, assetID, generation, rendition, parseErr := parsePlanningObjectKey(entry.Key)
		if parseErr != nil || entry.Key <= previousKey || entry.AssetID != assetID || entry.Generation != generation || entry.Rendition != rendition {
			cleanup(ctx, target, created)
			return fmt.Errorf("manifest_entry_invalid")
		}
		previousKey = entry.Key
		reader, err := openVerified(ctx, source, entry.Key, entry.Metadata)
		if err != nil {
			cleanup(ctx, target, created)
			return err
		}
		body, readErr := io.ReadAll(reader)
		_ = reader.Close()
		if readErr != nil {
			cleanup(ctx, target, created)
			return readErr
		}
		_, made, err := target.PutImmutable(ctx, entry.Key, body, entry.Metadata)
		if err != nil {
			cleanup(ctx, target, created)
			return err
		}
		if made {
			created = append(created, entry.Key)
		}
	}
	restored, err := BuildManifest(ctx, target)
	if err != nil || restored.Digest != manifest.Digest {
		cleanup(ctx, target, created)
		if err != nil {
			return err
		}
		return fmt.Errorf("restored_manifest_mismatch")
	}
	return nil
}

func ClassifyInventory(expected []ManifestEntry, actual []immutablefs.Item) []InventoryFinding {
	expectedByKey := make(map[string]ManifestEntry, len(expected))
	for _, entry := range expected {
		expectedByKey[entry.Key] = entry
	}
	actualByKey := make(map[string]immutablefs.Metadata, len(actual))
	for _, item := range actual {
		actualByKey[item.Key] = item.Meta
	}
	findings := make([]InventoryFinding, 0)
	for key, entry := range expectedByKey {
		meta, ok := actualByKey[key]
		if !ok {
			findings = append(findings, InventoryFinding{Key: key, Classification: InventoryMissing})
			continue
		}
		if !sameInventoryMetadata(entry.Metadata, meta) {
			findings = append(findings, InventoryFinding{Key: key, Classification: InventoryCorrupt})
		}
	}
	for key := range actualByKey {
		if _, ok := expectedByKey[key]; !ok {
			findings = append(findings, InventoryFinding{Key: key, Classification: InventoryOrphan})
		}
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Key == findings[j].Key {
			return findings[i].Classification < findings[j].Classification
		}
		return findings[i].Key < findings[j].Key
	})
	return findings
}

func sameInventoryMetadata(left, right immutablefs.Metadata) bool {
	return left.MediaType == right.MediaType && left.Size == right.Size && left.Checksum == right.Checksum &&
		left.Width == right.Width && left.Height == right.Height
}

func listAll(ctx context.Context, objects immutablefs.ObjectStore) ([]immutablefs.Item, error) {
	var all []immutablefs.Item
	cursor := ""
	for {
		page, err := objects.List(ctx, "", cursor, 1000)
		if err != nil {
			return nil, err
		}
		all = append(all, page.Items...)
		if page.Done {
			return all, nil
		}
		cursor = page.NextCursor
	}
}
func cleanup(ctx context.Context, target immutablefs.ObjectStore, keys []string) {
	for _, key := range keys {
		_ = target.Delete(ctx, key)
	}
}
func openVerified(ctx context.Context, source immutablefs.ObjectStore, key string, expected immutablefs.Metadata) (io.ReadCloser, error) {
	if verified, ok := source.(interface {
		OpenVerified(context.Context, string, immutablefs.Metadata) (io.ReadCloser, error)
	}); ok {
		return verified.OpenVerified(ctx, key, expected)
	}
	meta, err := source.Stat(ctx, key)
	if err != nil {
		return nil, err
	}
	if !sameInventoryMetadata(meta, expected) {
		return nil, immutablefs.ErrIntegrity
	}
	return source.Open(ctx, key)
}
func manifestDigest(manifest PlanningMediaManifestV1) string {
	clone := manifest
	clone.Digest = ""
	body, _ := json.Marshal(clone)
	sum := sha256.Sum256(body)
	return "sha256-" + hex.EncodeToString(sum[:])
}
