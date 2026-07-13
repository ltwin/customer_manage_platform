// Command avatar-manifest generates or verifies the stopped application's
// exact-generation customer avatar backup manifest.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	customerdomain "github.com/samson/customer-manage-platform/backend/internal/customer"
	"github.com/samson/customer-manage-platform/backend/internal/customer/avatarbackup"
	"github.com/samson/customer-manage-platform/backend/internal/customer/avatarstore"
	"github.com/samson/customer-manage-platform/backend/internal/platform/config"
	platformstore "github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, args []string, output io.Writer) error {
	flags := flag.NewFlagSet("avatar-manifest", flag.ContinueOnError)
	manifestPath := flags.String("manifest", "", "manifest file used by verify")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 || flags.Arg(0) != "generate" && flags.Arg(0) != "verify" {
		return errors.New("usage: avatar-manifest [--manifest FILE] generate|verify")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	database, err := platformstore.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer database.Close()
	objects, err := avatarstore.NewLocal(cfg.AvatarLocalRoot)
	if err != nil {
		return err
	}
	accounts, err := database.AccountScopes(ctx)
	if err != nil {
		return err
	}
	repository := customerdomain.PostgresAvatarRepository{}
	if flags.Arg(0) == "generate" {
		manifest, err := avatarbackup.Generate(ctx, accounts, repository, objects, time.Now())
		if err != nil {
			return err
		}
		encoder := json.NewEncoder(output)
		encoder.SetIndent("", "  ")
		return encoder.Encode(manifest)
	}
	if *manifestPath == "" {
		return errors.New("verify requires --manifest FILE")
	}
	file, err := os.Open(*manifestPath)
	if err != nil {
		return fmt.Errorf("open manifest: %w", err)
	}
	defer func() { _ = file.Close() }()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var expected avatarbackup.Manifest
	if err := decoder.Decode(&expected); err != nil {
		return fmt.Errorf("decode manifest: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("manifest must contain exactly one JSON document")
	}
	if err := avatarbackup.Verify(ctx, expected, accounts, repository, objects); err != nil {
		return err
	}
	_, err = fmt.Fprintln(output, "avatar exact-generation manifest verified")
	return err
}
