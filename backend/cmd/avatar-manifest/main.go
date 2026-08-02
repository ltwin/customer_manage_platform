// Command avatar-manifest generates or verifies the stopped application's
// exact-generation avatar backup manifest.
package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/accountprofile"
	"github.com/samson/customer-manage-platform/backend/internal/avatarmedia"
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
	profileRepo := accountprofile.NewPostgresRepository()
	profileSource := accountprofile.NewPointerSource(profileRepo)
	if flags.Arg(0) == "generate" {
		manifest, err := avatarbackup.Generate(ctx, accounts, repository, profileSource, objects, time.Now())
		if err != nil {
			return err
		}
		return avatarmedia.EncodeManifestCanonical(output, manifest)
	}
	if *manifestPath == "" {
		return errors.New("verify requires --manifest FILE")
	}
	raw, err := os.ReadFile(*manifestPath)
	if err != nil {
		return fmt.Errorf("open manifest: %w", err)
	}
	format, v1, v2, err := avatarmedia.DecodeStrictManifest(raw)
	if err != nil {
		return err
	}
	if err := avatarbackup.Verify(ctx, format, v1, v2, accounts, repository, profileSource, objects); err != nil {
		return err
	}
	// 禁止尾随空白以外的额外字节（DecodeStrictManifest 已校验单文档）。
	if len(bytes.TrimSpace(raw)) == 0 {
		return errors.New("manifest empty")
	}
	_, err = fmt.Fprintln(output, "avatar exact-generation manifest verified")
	return err
}
