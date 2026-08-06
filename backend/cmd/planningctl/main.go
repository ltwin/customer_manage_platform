// Command planningctl provides release-only planning deployment controls.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/planningcapability"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

type releaseInventory struct {
	Version                int                                  `json:"version"`
	Environment            string                               `json:"environment"`
	Deployment             string                               `json:"deployment"`
	SchemaHash             string                               `json:"schema_hash"`
	ContentHash            string                               `json:"content_hash"`
	GeneratedAt            time.Time                            `json:"generated_at"`
	ExpiresAt              time.Time                            `json:"expires_at"`
	Target                 planningcapability.ArchiveCapability `json:"target"`
	LiveBuilds             []string                             `json:"live_builds"`
	LiveBuildsCompatible   bool                                 `json:"live_builds_compatible"`
	TargetWiringReady      bool                                 `json:"target_wiring_ready"`
	ControlPlaneProvenance string                               `json:"control_plane_provenance"`
}

type readinessOutput struct {
	FrameVersion           int                                  `json:"frame_version"`
	Environment            string                               `json:"environment"`
	Deployment             string                               `json:"deployment"`
	SchemaHash             string                               `json:"schema_hash"`
	ContentHash            string                               `json:"content_hash"`
	GeneratedAt            time.Time                            `json:"generated_at"`
	ExpiresAt              time.Time                            `json:"expires_at"`
	Current                planningcapability.ArchiveCapability `json:"current"`
	CurrentRevision        int64                                `json:"current_revision"`
	Target                 planningcapability.ArchiveCapability `json:"target"`
	LiveBuilds             []string                             `json:"live_builds"`
	TargetWiringReady      bool                                 `json:"target_wiring_ready"`
	ControlPlaneProvenance string                               `json:"control_plane_provenance"`
	Digest                 string                               `json:"digest"`
}

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) < 2 || args[0] != "archive-capability" {
		return errors.New("usage: planningctl archive-capability {show|readiness|promote}")
	}
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return errors.New("DATABASE_URL 未设置")
	}
	db, err := store.Open(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer db.Close()
	promoter := db.ArchiveCapabilityPromoter()

	switch args[1] {
	case "show":
		state, err := promoter.Show(ctx)
		if err != nil {
			return err
		}
		return writeJSON(os.Stdout, state)
	case "readiness":
		fs := flag.NewFlagSet("readiness", flag.ContinueOnError)
		target := fs.String("target", "", "adjacent target capability")
		inventoryPath := fs.String("inventory", "", "trusted release inventory JSON")
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		state, err := promoter.Show(ctx)
		if err != nil {
			return err
		}
		output, _, err := buildReadiness(*inventoryPath, planningcapability.ArchiveCapability(*target), state, time.Now().UTC())
		if err != nil {
			return err
		}
		return writeJSON(os.Stdout, output)
	case "promote":
		fs := flag.NewFlagSet("promote", flag.ContinueOnError)
		target := fs.String("target", "", "adjacent target capability")
		inventoryPath := fs.String("inventory", "", "trusted release inventory JSON")
		expectedRevision := fs.Int64("expected-revision", 0, "current marker revision")
		readinessDigest := fs.String("readiness-digest", "", "digest emitted by readiness")
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		state, err := promoter.Show(ctx)
		if err != nil {
			return err
		}
		output, evidence, err := buildReadiness(*inventoryPath, planningcapability.ArchiveCapability(*target), state, time.Now().UTC())
		if err != nil {
			return err
		}
		if *readinessDigest == "" || output.Digest != *readinessDigest {
			return planningcapability.ErrReadiness
		}
		transition, err := promoter.Promote(ctx, *expectedRevision, planningcapability.ArchiveCapability(*target), evidence)
		if err != nil {
			return err
		}
		return writeJSON(os.Stdout, transition)
	default:
		return errors.New("unknown archive-capability command")
	}
}

func buildReadiness(
	inventoryPath string,
	target planningcapability.ArchiveCapability,
	current planningcapability.ArchiveCapabilityState,
	now time.Time,
) (readinessOutput, planningcapability.ReleaseReadinessEvidence, error) {
	if inventoryPath == "" || !planningcapability.Adjacent(current.Capability, target) {
		return readinessOutput{}, planningcapability.ReleaseReadinessEvidence{}, planningcapability.ErrReadiness
	}
	contents, err := os.ReadFile(inventoryPath)
	if err != nil {
		return readinessOutput{}, planningcapability.ReleaseReadinessEvidence{}, err
	}
	var inventory releaseInventory
	if err := json.Unmarshal(contents, &inventory); err != nil {
		return readinessOutput{}, planningcapability.ReleaseReadinessEvidence{}, fmt.Errorf("decode inventory: %w", err)
	}
	if inventory.Version != 1 || inventory.Target != target || inventory.Environment == "" ||
		inventory.Deployment == "" || inventory.SchemaHash == "" || inventory.ContentHash == "" ||
		inventory.ControlPlaneProvenance == "" || inventory.GeneratedAt.IsZero() || inventory.ExpiresAt.IsZero() ||
		now.Before(inventory.GeneratedAt) || !now.Before(inventory.ExpiresAt) ||
		!inventory.LiveBuildsCompatible || !inventory.TargetWiringReady || len(inventory.LiveBuilds) == 0 {
		return readinessOutput{}, planningcapability.ReleaseReadinessEvidence{}, planningcapability.ErrReadiness
	}
	builds := append([]string(nil), inventory.LiveBuilds...)
	sort.Strings(builds)
	for index, build := range builds {
		if build == "" || index > 0 && build == builds[index-1] {
			return readinessOutput{}, planningcapability.ReleaseReadinessEvidence{}, planningcapability.ErrReadiness
		}
	}
	output := readinessOutput{
		FrameVersion: 1, Environment: inventory.Environment, Deployment: inventory.Deployment,
		SchemaHash: inventory.SchemaHash, ContentHash: inventory.ContentHash,
		GeneratedAt: inventory.GeneratedAt.UTC(), ExpiresAt: inventory.ExpiresAt.UTC(),
		Current: current.Capability, CurrentRevision: current.Revision, Target: target,
		LiveBuilds: builds, TargetWiringReady: true,
		ControlPlaneProvenance: inventory.ControlPlaneProvenance,
	}
	canonical, err := json.Marshal(output)
	if err != nil {
		return readinessOutput{}, planningcapability.ReleaseReadinessEvidence{}, err
	}
	digest := sha256.Sum256(canonical)
	output.Digest = hex.EncodeToString(digest[:])
	evidence := planningcapability.ReleaseReadinessEvidence{
		Digest: output.Digest, Environment: output.Environment, Deployment: output.Deployment,
		Target: target, CurrentRevision: current.Revision, GeneratedAt: output.GeneratedAt,
		ExpiresAt: output.ExpiresAt, LiveBuildsCompatible: true, TargetWiringReady: true,
	}
	return output, evidence, nil
}

func writeJSON(file *os.File, value any) error {
	encoder := json.NewEncoder(file)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(value)
}
