// Command planning-media-manifest generates or verifies the stopped
// application's exact planning-media volume inventory.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/samson/customer-manage-platform/backend/internal/planningmedia"
	"github.com/samson/customer-manage-platform/backend/internal/platform/immutablefs"
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "planning-media-manifest error: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("planning-media-manifest", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	root := flags.String("root", os.Getenv("PLANNING_MEDIA_LOCAL_ROOT"), "planning media volume root")
	manifestPath := flags.String("manifest", "", "manifest file used by verify")
	if err := flags.Parse(args); err != nil {
		return errors.New("usage: planning-media-manifest [--root ROOT] [--manifest FILE] generate|verify")
	}
	if *root == "" {
		*root = "/var/lib/crm/planning-media"
	}
	if filepath.IsAbs(*root) && filepath.Clean(*root) == string(filepath.Separator) {
		return errors.New("root must not be filesystem root")
	}
	objects, err := immutablefs.NewLocal(*root)
	if err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return errors.New("usage: planning-media-manifest [--root ROOT] [--manifest FILE] generate|verify")
	}
	manifest, err := planningmedia.BuildManifest(ctx, objects)
	if err != nil {
		return err
	}
	switch flags.Arg(0) {
	case "generate":
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetEscapeHTML(false)
		return encoder.Encode(manifest)
	case "verify":
		if *manifestPath == "" {
			return errors.New("verify requires --manifest FILE")
		}
		file, err := os.Open(*manifestPath)
		if err != nil {
			return err
		}
		defer func() { _ = file.Close() }()
		var expected planningmedia.PlanningMediaManifestV1
		decoder := json.NewDecoder(file)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&expected); err != nil {
			return fmt.Errorf("decode manifest: %w", err)
		}
		if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
			return errors.New("manifest has trailing data")
		}
		if expected.Version != 1 || expected.Digest == "" || expected.Digest != manifest.Digest {
			return errors.New("planning media manifest mismatch")
		}
		_, _ = fmt.Fprintln(os.Stdout, "planning media exact manifest verified")
		return nil
	default:
		return errors.New("unknown command")
	}
}
