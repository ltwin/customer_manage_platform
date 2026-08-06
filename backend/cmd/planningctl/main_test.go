package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/planningcapability"
)

func TestBuildReadinessBindsDeploymentAndSortsBuilds(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	inventory := releaseInventory{
		Version: 1, Environment: "staging-cn", Deployment: "planning-staging-01",
		SchemaHash: "schema", ContentHash: "content", GeneratedAt: now.Add(-time.Minute),
		ExpiresAt: now.Add(time.Hour), Target: planningcapability.ArchiveCapabilityPlanningShare,
		LiveBuilds: []string{"build-b", "build-a"}, LiveBuildsCompatible: true,
		TargetWiringReady: true, ControlPlaneProvenance: "release-controller/least-privilege",
	}
	contents, err := json.Marshal(inventory)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "inventory.json")
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	current := planningcapability.ArchiveCapabilityState{
		SingletonKey: planningcapability.SingletonKey,
		Capability:   planningcapability.ArchiveCapabilityCore,
		Revision:     1,
		PromotedAt:   now.Add(-time.Hour),
	}
	output, evidence, err := buildReadiness(path, planningcapability.ArchiveCapabilityPlanningShare, current, now)
	if err != nil {
		t.Fatalf("buildReadiness: %v", err)
	}
	if output.Digest == "" || output.LiveBuilds[0] != "build-a" || evidence.Digest != output.Digest {
		t.Fatalf("unexpected readiness: output=%+v evidence=%+v", output, evidence)
	}

	inventory.Deployment = ""
	contents, _ = json.Marshal(inventory)
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := buildReadiness(path, planningcapability.ArchiveCapabilityPlanningShare, current, now); err == nil {
		t.Fatal("deployment-less readiness must fail")
	}
}
