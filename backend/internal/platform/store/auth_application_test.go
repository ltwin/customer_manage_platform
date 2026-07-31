package store_test

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/netip"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func TestPasswordResetAndChangeRevokeAllRefreshFamilies(t *testing.T) {
	url := startPostgres(t)
	database := resetAuthDatabase(t, url)
	now := time.Date(2026, 7, 31, 10, 0, 0, 0, time.UTC)
	mail := &capturingAuthMail{}
	service := newTestAuthService(database, &now, mail, 1, auth.RegistrationPublic, "synthetic-root")
	email := syntheticEmail("password-owner")
	meta := auth.ClientMeta{SourceIP: netip.MustParseAddr("198.51.100.7")}

	if _, err := service.Register(context.Background(), email, "original-password", testClientMeta()); err != nil {
		t.Fatalf("register password owner: %v", err)
	}
	firstSession, err := service.VerifyEmail(context.Background(), actionTokenFromMail(t, mail.Last()), testClientMeta())
	if err != nil {
		t.Fatalf("verify password owner: %v", err)
	}
	secondSession, err := service.Login(context.Background(), email, "original-password", testClientMeta())
	if err != nil {
		t.Fatalf("create second password session: %v", err)
	}

	firstDispatch, err := service.BeginPasswordReset(context.Background(), email, meta)
	if err != nil || !firstDispatch.Attempted || !firstDispatch.Delivery.Accepted {
		t.Fatalf("begin first password reset: %#v err=%v", firstDispatch, err)
	}
	firstResetToken := actionTokenFromMail(t, mail.Last())
	secondDispatch, err := service.BeginPasswordReset(context.Background(), email, meta)
	if err != nil || !secondDispatch.Attempted || !secondDispatch.Delivery.Accepted {
		t.Fatalf("begin second password reset: %#v err=%v", secondDispatch, err)
	}
	secondResetToken := actionTokenFromMail(t, mail.Last())
	if err := service.ResetPassword(context.Background(), firstResetToken, "replacement-password", meta); !auth.IsAuthError(err, auth.AuthErrorInvalidOrExpiredToken) {
		t.Fatalf("replaced reset token must be invalid: %v", err)
	}
	if err := service.ResetPassword(context.Background(), secondResetToken, "replacement-password", meta); err != nil {
		t.Fatalf("reset password: %v", err)
	}
	if _, err := service.Login(context.Background(), email, "original-password", testClientMeta()); !auth.IsAuthError(err, auth.AuthErrorUnauthorized) {
		t.Fatalf("old password must be unauthorized: %v", err)
	}
	if _, err := service.Refresh(context.Background(), firstSession.RefreshToken, testClientMeta()); !auth.IsAuthError(err, auth.AuthErrorUnauthorized) {
		t.Fatalf("first refresh family must be revoked: %v", err)
	}
	if _, err := service.Refresh(context.Background(), secondSession.RefreshToken, testClientMeta()); !auth.IsAuthError(err, auth.AuthErrorUnauthorized) {
		t.Fatalf("second refresh family must be revoked: %v", err)
	}
	if accountID, err := service.ParseToken(firstSession.AccessToken); err != nil || accountID != firstSession.AccountID {
		t.Fatalf("existing access keeps the accepted TTL risk window: account=%q err=%v", accountID, err)
	}

	thirdSession, err := service.Login(context.Background(), email, "replacement-password", testClientMeta())
	if err != nil {
		t.Fatalf("login with replacement password: %v", err)
	}
	account := auth.AccountContext{AccountID: thirdSession.AccountID}
	if err := service.ChangePassword(context.Background(), account, "wrong-current-password", "final-password", meta); !auth.IsAuthError(err, auth.AuthErrorUnauthorized) {
		t.Fatalf("wrong current password must be unauthorized: %v", err)
	}
	thirdSession, err = service.Refresh(context.Background(), thirdSession.RefreshToken, testClientMeta())
	if err != nil {
		t.Fatalf("wrong current password must not revoke refresh family: %v", err)
	}
	if err := service.ChangePassword(context.Background(), account, "replacement-password", "final-password", meta); err != nil {
		t.Fatalf("change password: %v", err)
	}
	if _, err := service.Refresh(context.Background(), thirdSession.RefreshToken, testClientMeta()); !auth.IsAuthError(err, auth.AuthErrorUnauthorized) {
		t.Fatalf("change password must revoke current refresh family: %v", err)
	}
	if _, err := service.Login(context.Background(), email, "final-password", testClientMeta()); err != nil {
		t.Fatalf("login with final password: %v", err)
	}

	if _, err := service.BeginPasswordReset(context.Background(), email, meta); err != nil {
		t.Fatalf("begin expiring reset: %v", err)
	}
	expiringToken := actionTokenFromMail(t, mail.Last())
	now = now.Add(30 * time.Minute)
	if err := service.ResetPassword(context.Background(), expiringToken, "expired-password", meta); !auth.IsAuthError(err, auth.AuthErrorInvalidOrExpiredToken) {
		t.Fatalf("reset token at exact 30m boundary must be expired: %v", err)
	}
}

func TestBeginPasswordResetIsGenericAndResetTokensArePurposeBound(t *testing.T) {
	url := startPostgres(t)
	database := resetAuthDatabase(t, url)
	now := time.Date(2026, 7, 31, 11, 0, 0, 0, time.UTC)
	mail := &capturingAuthMail{}
	service := newTestAuthService(database, &now, mail, 71, auth.RegistrationPublic, "synthetic-root")
	meta := auth.ClientMeta{SourceIP: netip.MustParseAddr("198.51.100.8")}

	missing, err := service.BeginPasswordReset(context.Background(), syntheticEmail("missing"), meta)
	if err != nil || missing.Attempted || mail.Count() != 0 {
		t.Fatalf("missing forgot outcome must be generic: %#v err=%v mail=%d", missing, err, mail.Count())
	}
	pendingEmail := syntheticEmail("pending-password")
	if _, err := service.Register(context.Background(), pendingEmail, "original-password", testClientMeta()); err != nil {
		t.Fatalf("register pending account: %v", err)
	}
	verificationToken := actionTokenFromMail(t, mail.Last())
	pending, err := service.BeginPasswordReset(context.Background(), pendingEmail, meta)
	if err != nil || pending.Attempted || mail.Count() != 1 {
		t.Fatalf("pending forgot outcome must be generic: %#v err=%v mail=%d", pending, err, mail.Count())
	}
	if err := service.ResetPassword(context.Background(), verificationToken, "replacement-password", meta); !auth.IsAuthError(err, auth.AuthErrorInvalidOrExpiredToken) {
		t.Fatalf("verification token must not reset password: %v", err)
	}

	if _, err := service.VerifyEmail(context.Background(), verificationToken, testClientMeta()); err != nil {
		t.Fatalf("verify active reset fixture: %v", err)
	}
	mail.SetError(auth.NewDeliveryError(auth.DeliveryTemporarilyUnavailable))
	dispatch, err := service.BeginPasswordReset(context.Background(), pendingEmail, meta)
	if err != nil || !dispatch.Attempted || dispatch.Delivery.Accepted ||
		dispatch.Delivery.FailureClass != auth.DeliveryTemporarilyUnavailable {
		t.Fatalf("delivery failure must preserve generic reset state: %#v err=%v", dispatch, err)
	}
	resetToken := actionTokenFromMail(t, mail.Last())
	if err := service.ResetPassword(context.Background(), resetToken+"tamper", "replacement-password", meta); !auth.IsAuthError(err, auth.AuthErrorInvalidOrExpiredToken) {
		t.Fatalf("tampered reset token must be invalid: %v", err)
	}
	if err := service.ResetPassword(context.Background(), resetToken, "replacement-password", meta); err != nil {
		t.Fatalf("valid reset token after delivery failure: %v", err)
	}
	if err := service.ResetPassword(context.Background(), resetToken, "another-password", meta); !auth.IsAuthError(err, auth.AuthErrorInvalidOrExpiredToken) {
		t.Fatalf("consumed reset token must be invalid: %v", err)
	}
}

func TestAccountAuthApplicationPostgres(t *testing.T) {
	url := startPostgres(t)
	t.Run("registration verification and current account", func(t *testing.T) {
		database := resetAuthDatabase(t, url)
		now := time.Date(2026, 7, 31, 2, 3, 4, 0, time.UTC)
		mail := &capturingAuthMail{}
		service := newTestAuthService(database, &now, mail, 1, auth.RegistrationPublic, "synthetic-root")
		email := syntheticEmail("owner")

		plan, err := service.PlanBootstrap(context.Background(), email, "twelve-bytes")
		if err != nil || !plan.Eligible || plan.RecipientRef == "" || plan.RecipientRef == email {
			t.Fatalf("bootstrap plan: %#v err=%v", plan, err)
		}
		assertAuthTableCounts(t, url, 0, 0, 0)
		if mail.Count() != 0 {
			t.Fatal("bootstrap dry-run attempted mail delivery")
		}

		dispatch, err := service.Register(context.Background(), email, "twelve-bytes", testClientMeta())
		if err != nil || !dispatch.Attempted || !dispatch.Delivery.Accepted {
			t.Fatalf("register dispatch: %#v err=%v", dispatch, err)
		}
		duplicate, err := service.Register(context.Background(), email, "twelve-bytes", testClientMeta())
		if err != nil || duplicate.Attempted || mail.Count() != 1 {
			t.Fatalf("duplicate register must be generic no-dispatch: %#v err=%v mail=%d", duplicate, err, mail.Count())
		}
		if _, err := service.Login(context.Background(), email, "wrong-password", testClientMeta()); !auth.IsAuthError(err, auth.AuthErrorUnauthorized) {
			t.Fatalf("wrong password should be unauthorized: %v", err)
		}
		if _, err := service.Login(context.Background(), email, "twelve-bytes", testClientMeta()); !auth.IsAuthError(err, auth.AuthErrorEmailVerificationRequired) {
			t.Fatalf("pending account should require verification after password match: %v", err)
		}

		verification := actionTokenFromMail(t, mail.Last())
		session, err := service.VerifyEmail(context.Background(), verification, testClientMeta())
		if err != nil {
			t.Fatalf("verify email: %v", err)
		}
		if !session.AccessExpiresAt.Equal(now.Add(10*time.Minute)) ||
			!session.RefreshExpiresAt.Equal(now.Add(14*24*time.Hour)) ||
			!session.RefreshAbsoluteAt.Equal(now.Add(30*24*time.Hour)) {
			t.Fatalf("session TTL contract mismatch: access=%s idle=%s absolute=%s",
				session.AccessExpiresAt, session.RefreshExpiresAt, session.RefreshAbsoluteAt)
		}
		if _, err := service.VerifyEmail(context.Background(), verification, testClientMeta()); !auth.IsAuthError(err, auth.AuthErrorInvalidOrExpiredToken) {
			t.Fatalf("consumed action token should be rejected: %v", err)
		}
		accountID, err := service.ParseToken(session.AccessToken)
		if err != nil || accountID == "" {
			t.Fatalf("parse access token: account=%q err=%v", accountID, err)
		}
		account, err := service.CurrentAccount(context.Background(), accountID)
		if err != nil || account.ID != accountID || account.Email != email || account.Status != auth.AccountActive {
			t.Fatalf("current account: %#v err=%v", account, err)
		}
		if _, err := service.Login(context.Background(), email, "twelve-bytes", testClientMeta()); err != nil {
			t.Fatalf("active account login: %v", err)
		}
		if err := database.CreateAccount(context.Background(), "legacy-account", "legacy-hash"); err != nil {
			t.Fatalf("create legacy account fixture: %v", err)
		}
		scopes, err := database.AccountScopes(context.Background())
		if err != nil {
			t.Fatalf("list account scopes: %v", err)
		}
		if len(scopes) != 1 || scopes[0].AccountID != accountID {
			t.Fatalf("AccountScopes must contain only active accounts: %#v", scopes)
		}
		assertStoredBearerHashes(t, url)
	})

	t.Run("action expiry and legacy retry", func(t *testing.T) {
		database := resetAuthDatabase(t, url)
		now := time.Date(2026, 7, 31, 3, 4, 5, 0, time.UTC)
		mail := &capturingAuthMail{}
		service := newTestAuthService(database, &now, mail, 17, auth.RegistrationPublic, "synthetic-root")
		if _, err := service.Register(context.Background(), syntheticEmail("expiring"), "twelve-bytes", testClientMeta()); err != nil {
			t.Fatalf("register expiring account: %v", err)
		}
		expiringToken := actionTokenFromMail(t, mail.Last())
		now = now.Add(24 * time.Hour)
		if _, err := service.VerifyEmail(context.Background(), expiringToken, testClientMeta()); !auth.IsAuthError(err, auth.AuthErrorInvalidOrExpiredToken) {
			t.Fatalf("token at exact 24h boundary should be expired: %v", err)
		}

		database = resetAuthDatabase(t, url)
		now = time.Date(2026, 7, 31, 4, 5, 6, 0, time.UTC)
		mail = &capturingAuthMail{sendErr: auth.NewDeliveryError(auth.DeliveryMisconfigured)}
		service = newTestAuthService(database, &now, mail, 33, auth.RegistrationPublic, "synthetic-root")
		const legacyID = "legacy-stable-id"
		if err := database.CreateAccount(context.Background(), legacyID, "legacy-hash"); err != nil {
			t.Fatalf("create legacy account: %v", err)
		}
		claimEmail := syntheticEmail("legacy")
		dryRun, err := service.BeginLegacyClaim(context.Background(), claimEmail, true)
		if err != nil || !dryRun.DryRun || dryRun.State != auth.LegacyClaimReady ||
			dryRun.AccountIDRedacted == "" || dryRun.AccountIDRedacted == legacyID ||
			dryRun.EmailRedacted == "" || dryRun.EmailRedacted == claimEmail ||
			dryRun.LegacyCount != 1 || dryRun.PendingClaimCount != 0 {
			t.Fatalf("legacy claim dry-run: %#v err=%v", dryRun, err)
		}
		assertAuthTableCounts(t, url, 1, 0, 0)
		if mail.Count() != 0 {
			t.Fatal("legacy claim dry-run attempted mail delivery")
		}

		first, err := service.BeginLegacyClaim(context.Background(), claimEmail, false)
		if err != nil || first.DryRun || first.State != auth.LegacyClaimReady ||
			!first.Delivery.Attempted || first.Delivery.FailureClass != auth.DeliveryMisconfigured {
			t.Fatalf("first legacy claim: %#v err=%v", first, err)
		}
		firstToken := actionTokenFromMail(t, mail.Last())
		second, err := service.BeginLegacyClaim(context.Background(), claimEmail, false)
		if err != nil || second.State != auth.LegacyClaimPendingSameEmail ||
			!second.Delivery.Attempted || mail.Count() != 2 {
			t.Fatalf("same-email legacy retry: %#v err=%v mail=%d", second, err, mail.Count())
		}
		secondToken := actionTokenFromMail(t, mail.Last())
		if firstToken == secondToken {
			t.Fatal("legacy retry must replace the prior action token")
		}
		mail.sendErr = nil
		if _, err := service.VerifyEmail(context.Background(), firstToken, testClientMeta()); !auth.IsAuthError(err, auth.AuthErrorInvalidOrExpiredToken) {
			t.Fatalf("replaced legacy token should be invalid: %v", err)
		}
		if _, err := service.VerifyEmail(context.Background(), secondToken, testClientMeta()); err != nil {
			t.Fatalf("verify retried legacy claim: %v", err)
		}
		state, err := service.InspectLegacyState(context.Background())
		if err != nil || state.LegacyUnclaimedCount != 0 || state.PendingClaimCount != 0 || state.ActiveClaimedCount != 1 {
			t.Fatalf("legacy state after claim: %#v err=%v", state, err)
		}
		completed, err := service.BeginLegacyClaim(context.Background(), claimEmail, true)
		if err != nil || completed.State != auth.LegacyClaimAlreadyClaimed || completed.AccountIDRedacted == "" {
			t.Fatalf("completed legacy claim state: %#v err=%v", completed, err)
		}
		var accountCount int
		var persistedID string
		if err := queryAuthDB(t, url).QueryRow(`SELECT count(*), min(id) FROM accounts`).Scan(&accountCount, &persistedID); err != nil {
			t.Fatalf("inspect claimed legacy account: %v", err)
		}
		if accountCount != 1 || persistedID != legacyID {
			t.Fatalf("legacy claim changed account identity: count=%d id=%q", accountCount, persistedID)
		}
	})

	t.Run("legacy claim no-target and conflict states", func(t *testing.T) {
		database := resetAuthDatabase(t, url)
		now := time.Date(2026, 7, 31, 4, 20, 0, 0, time.UTC)
		mail := &capturingAuthMail{}
		service := newTestAuthService(database, &now, mail, 37, auth.RegistrationPublic, "synthetic-root")

		noTarget, err := service.BeginLegacyClaim(context.Background(), syntheticEmail("missing"), true)
		if err != nil || noTarget.State != auth.LegacyClaimNoTarget || noTarget.LegacyCount != 0 {
			t.Fatalf("legacy no-target: %#v err=%v", noTarget, err)
		}
		if err := database.CreateAccount(context.Background(), "legacy-a", "legacy-hash-a"); err != nil {
			t.Fatalf("create first conflicting legacy account: %v", err)
		}
		if err := database.CreateAccount(context.Background(), "legacy-b", "legacy-hash-b"); err != nil {
			t.Fatalf("create second conflicting legacy account: %v", err)
		}
		conflict, err := service.BeginLegacyClaim(context.Background(), syntheticEmail("owner"), true)
		if err != nil || conflict.State != auth.LegacyClaimConflict || conflict.LegacyCount != 2 {
			t.Fatalf("legacy conflict: %#v err=%v", conflict, err)
		}
		if mail.Count() != 0 {
			t.Fatal("non-ready legacy states attempted mail delivery")
		}
		assertAuthTableCounts(t, url, 2, 0, 0)
	})

	t.Run("resend replacement and generic outcomes", func(t *testing.T) {
		database := resetAuthDatabase(t, url)
		now := time.Date(2026, 7, 31, 4, 30, 0, 0, time.UTC)
		mail := &capturingAuthMail{}
		service := newTestAuthService(database, &now, mail, 41, auth.RegistrationPublic, "synthetic-root")
		email := syntheticEmail("resend")
		if _, err := service.Register(context.Background(), email, "twelve-bytes", testClientMeta()); err != nil {
			t.Fatalf("register resend account: %v", err)
		}
		firstToken := actionTokenFromMail(t, mail.Last())
		mail.SetError(auth.NewDeliveryError(auth.DeliveryProviderRejected))
		dispatch, err := service.ResendVerification(context.Background(), email, testClientMeta())
		if err != nil || !dispatch.Attempted || dispatch.Delivery.FailureClass != auth.DeliveryProviderRejected {
			t.Fatalf("resend delivery failure should be absorbed: attempted=%t class=%q err=%v",
				dispatch.Attempted, dispatch.Delivery.FailureClass, err)
		}
		secondToken := actionTokenFromMail(t, mail.Last())
		if secondToken == firstToken {
			t.Fatal("resend must replace the prior verification token")
		}
		if _, err := service.VerifyEmail(context.Background(), firstToken, testClientMeta()); !auth.IsAuthError(err, auth.AuthErrorInvalidOrExpiredToken) {
			t.Fatalf("replaced verification token should be invalid: %v", err)
		}
		if _, err := service.VerifyEmail(context.Background(), secondToken, testClientMeta()); err != nil {
			t.Fatalf("replacement token must remain usable after delivery failure: %v", err)
		}
		before := mail.Count()
		for _, target := range []string{email, syntheticEmail("missing")} {
			result, err := service.ResendVerification(context.Background(), target, testClientMeta())
			if err != nil || result.Attempted {
				t.Fatalf("resend non-pending target must be generic no-dispatch: attempted=%t err=%v", result.Attempted, err)
			}
		}
		if mail.Count() != before {
			t.Fatalf("generic no-target resend attempted delivery: before=%d after=%d", before, mail.Count())
		}
	})

	t.Run("refresh state machine", func(t *testing.T) {
		database := resetAuthDatabase(t, url)
		now := time.Date(2026, 7, 31, 5, 6, 7, 0, time.UTC)
		mail := &capturingAuthMail{}
		service := newTestAuthService(database, &now, mail, 49, auth.RegistrationPublic, "synthetic-root")
		email := syntheticEmail("refresh")
		if _, err := service.Register(context.Background(), email, "twelve-bytes", testClientMeta()); err != nil {
			t.Fatalf("register refresh account: %v", err)
		}
		initial, err := service.VerifyEmail(context.Background(), actionTokenFromMail(t, mail.Last()), testClientMeta())
		if err != nil {
			t.Fatalf("verify refresh account: %v", err)
		}
		now = now.Add(time.Second)
		rotated, err := service.Refresh(context.Background(), initial.RefreshToken, testClientMeta())
		if err != nil {
			t.Fatalf("first refresh: %v", err)
		}
		now = now.Add(10 * time.Second)
		grace, err := service.Refresh(context.Background(), initial.RefreshToken, testClientMeta())
		if err != nil || grace.RefreshToken != rotated.RefreshToken || grace.RefreshGeneration != rotated.RefreshGeneration {
			t.Fatalf("grace replay must return the same successor: same_token=%t same_generation=%t err=%v",
				grace.RefreshToken == rotated.RefreshToken, grace.RefreshGeneration == rotated.RefreshGeneration, err)
		}
		now = now.Add(time.Microsecond)
		if _, err := service.Refresh(context.Background(), initial.RefreshToken, testClientMeta()); !auth.IsAuthError(err, auth.AuthErrorUnauthorized) {
			t.Fatalf("refresh reuse outside grace should revoke family: %v", err)
		}
		if _, err := service.Refresh(context.Background(), rotated.RefreshToken, testClientMeta()); !auth.IsAuthError(err, auth.AuthErrorUnauthorized) {
			t.Fatalf("successor from revoked family should be invalid: %v", err)
		}

		sweepParent := mustLogin(t, service, email)
		now = now.Add(time.Minute)
		if _, err := service.Refresh(context.Background(), sweepParent.RefreshToken, testClientMeta()); err != nil {
			t.Fatalf("create sweep replay envelope: %v", err)
		}
		now = now.Add(11 * time.Second)
		cleared, err := service.SweepExpiredReplayCiphertexts(context.Background(), 10)
		if err != nil || cleared < 1 {
			t.Fatalf("sweep expired replay envelopes: cleared=%d err=%v", cleared, err)
		}
		var remaining int
		if err := queryAuthDB(t, url).QueryRow(`SELECT count(*) FROM refresh_session_generations WHERE replay_ciphertext IS NOT NULL AND replay_until < $1`, now).Scan(&remaining); err != nil || remaining != 0 {
			t.Fatalf("sweep left expired ciphertexts: remaining=%d err=%v", remaining, err)
		}

		tamperParent := mustLogin(t, service, email)
		now = now.Add(time.Minute)
		if _, err := service.Refresh(context.Background(), tamperParent.RefreshToken, testClientMeta()); err != nil {
			t.Fatalf("create tamper replay envelope: %v", err)
		}
		if _, err := queryAuthDB(t, url).Exec(`
			UPDATE refresh_session_generations
			SET replay_ciphertext = set_byte(replay_ciphertext, octet_length(replay_ciphertext)-1,
				get_byte(replay_ciphertext, octet_length(replay_ciphertext)-1) # 1)
			WHERE id = $1`, tamperParent.RefreshGeneration); err != nil {
			t.Fatalf("tamper replay envelope: %v", err)
		}
		if _, err := service.Refresh(context.Background(), tamperParent.RefreshToken, testClientMeta()); !auth.IsAuthError(err, auth.AuthErrorUnauthorized) {
			t.Fatalf("tampered replay should be unauthorized: %v", err)
		}
		assertFamilyRevoked(t, url, tamperParent.RefreshSessionID)

		keyRotationParent := mustLogin(t, service, email)
		now = now.Add(time.Minute)
		if _, err := service.Refresh(context.Background(), keyRotationParent.RefreshToken, testClientMeta()); err != nil {
			t.Fatalf("create key-rotation replay envelope: %v", err)
		}
		rotatedRootService := newTestAuthService(database, &now, &capturingAuthMail{}, 113, auth.RegistrationPublic, "rotated-synthetic-root")
		if _, err := rotatedRootService.Refresh(context.Background(), keyRotationParent.RefreshToken, testClientMeta()); !auth.IsAuthError(err, auth.AuthErrorUnauthorized) {
			t.Fatalf("old replay under rotated root should be unauthorized: %v", err)
		}
		assertFamilyRevoked(t, url, keyRotationParent.RefreshSessionID)

		absoluteStart := now.Add(time.Hour)
		now = absoluteStart
		absolute := mustLogin(t, service, email)
		now = absoluteStart.Add(13 * 24 * time.Hour)
		absolute = mustRefresh(t, service, absolute.RefreshToken)
		now = absoluteStart.Add(26 * 24 * time.Hour)
		absolute = mustRefresh(t, service, absolute.RefreshToken)
		now = absoluteStart.Add(30*24*time.Hour - time.Hour)
		absolute = mustRefresh(t, service, absolute.RefreshToken)
		if !absolute.RefreshExpiresAt.Equal(absoluteStart.Add(30*24*time.Hour)) ||
			!absolute.RefreshAbsoluteAt.Equal(absoluteStart.Add(30*24*time.Hour)) {
			t.Fatalf("near-absolute refresh must be shortened: idle=%s absolute=%s want=%s",
				absolute.RefreshExpiresAt, absolute.RefreshAbsoluteAt, absoluteStart.Add(30*24*time.Hour))
		}
		now = absoluteStart.Add(30 * 24 * time.Hour)
		if _, err := service.Refresh(context.Background(), absolute.RefreshToken, testClientMeta()); !auth.IsAuthError(err, auth.AuthErrorUnauthorized) {
			t.Fatalf("refresh at absolute expiry should be unauthorized: %v", err)
		}

		logout := mustLogin(t, service, email)
		if err := service.Logout(context.Background(), ""); err != nil {
			t.Fatalf("logout without token: %v", err)
		}
		if err := service.Logout(context.Background(), logout.RefreshToken); err != nil {
			t.Fatalf("logout valid token: %v", err)
		}
		if err := service.Logout(context.Background(), logout.RefreshToken); err != nil {
			t.Fatalf("logout already revoked token: %v", err)
		}
		if _, err := service.Refresh(context.Background(), logout.RefreshToken, testClientMeta()); !auth.IsAuthError(err, auth.AuthErrorUnauthorized) {
			t.Fatalf("post-logout refresh should be unauthorized: %v", err)
		}
	})

	t.Run("concurrent bootstrap admission", func(t *testing.T) {
		database := resetAuthDatabase(t, url)
		now := time.Date(2026, 7, 31, 6, 7, 8, 0, time.UTC)
		services := []*auth.Service{
			newTestAuthService(database, &now, &capturingAuthMail{}, 129, auth.RegistrationBootstrapFirstAccount, "synthetic-root"),
			newTestAuthService(database, &now, &capturingAuthMail{}, 193, auth.RegistrationBootstrapFirstAccount, "synthetic-root"),
		}
		start := make(chan struct{})
		type result struct {
			dispatch auth.DispatchResult
			err      error
		}
		results := make(chan result, 2)
		for index, service := range services {
			index, service := index, service
			go func() {
				<-start
				dispatch, err := service.Register(context.Background(), syntheticEmail(string(rune('a'+index))), "twelve-bytes", testClientMeta())
				results <- result{dispatch: dispatch, err: err}
			}()
		}
		close(start)
		successes, losers := 0, 0
		for range services {
			result := <-results
			switch {
			case result.err == nil && result.dispatch.Attempted:
				successes++
			case auth.IsAuthError(result.err, auth.AuthErrorBootstrapNotEmpty):
				losers++
			default:
				t.Fatalf("unexpected bootstrap result: %#v err=%v", result.dispatch, result.err)
			}
		}
		if successes != 1 || losers != 1 {
			t.Fatalf("bootstrap concurrency: successes=%d losers=%d", successes, losers)
		}
		assertAuthTableCounts(t, url, 1, 1, 1)
	})

	t.Run("concurrent public duplicate email", func(t *testing.T) {
		database := resetAuthDatabase(t, url)
		now := time.Date(2026, 7, 31, 6, 30, 0, 0, time.UTC)
		services := []*auth.Service{
			newTestAuthService(database, &now, &capturingAuthMail{}, 11, auth.RegistrationPublic, "synthetic-root"),
			newTestAuthService(database, &now, &capturingAuthMail{}, 91, auth.RegistrationPublic, "synthetic-root"),
		}
		start := make(chan struct{})
		results := make(chan registrationResult, 2)
		for _, service := range services {
			service := service
			go func() {
				<-start
				dispatch, err := service.Register(context.Background(), syntheticEmail("same"), "twelve-bytes", testClientMeta())
				results <- registrationResult{dispatch: dispatch, err: err}
			}()
		}
		close(start)
		attempted := 0
		for range services {
			result := <-results
			if result.err != nil {
				t.Fatalf("public duplicate registration must stay generic: %v", result.err)
			}
			if result.dispatch.Attempted {
				attempted++
			}
		}
		if attempted != 1 {
			t.Fatalf("concurrent duplicate registration deliveries: got=%d want=1", attempted)
		}
		assertAuthTableCounts(t, url, 1, 1, 1)
	})

	t.Run("public holds admission before bootstrap", func(t *testing.T) {
		holder, contender := runMixedAdmissionBarrier(
			t, url, auth.RegistrationPublic, auth.RegistrationBootstrapFirstAccount,
		)
		if holder.err != nil || !holder.dispatch.Attempted {
			t.Fatalf("public holder should create the account: attempted=%t err=%v", holder.dispatch.Attempted, holder.err)
		}
		if !auth.IsAuthError(contender.err, auth.AuthErrorBootstrapNotEmpty) || contender.dispatch.Attempted {
			t.Fatalf("bootstrap contender should observe committed public account: attempted=%t err=%v",
				contender.dispatch.Attempted, contender.err)
		}
		assertAuthTableCounts(t, url, 1, 1, 1)
	})

	t.Run("bootstrap holds admission before public", func(t *testing.T) {
		holder, contender := runMixedAdmissionBarrier(
			t, url, auth.RegistrationBootstrapFirstAccount, auth.RegistrationPublic,
		)
		if holder.err != nil || !holder.dispatch.Attempted {
			t.Fatalf("bootstrap holder should create the first account: attempted=%t err=%v", holder.dispatch.Attempted, holder.err)
		}
		if contender.err != nil || !contender.dispatch.Attempted {
			t.Fatalf("public contender should continue only after bootstrap commit: attempted=%t err=%v",
				contender.dispatch.Attempted, contender.err)
		}
		assertAuthTableCounts(t, url, 2, 2, 2)
	})
}

const authTestBarrierLockID int64 = 731_731_731

type registrationResult struct {
	dispatch auth.DispatchResult
	err      error
}

func runMixedAdmissionBarrier(
	t *testing.T,
	databaseURL string,
	holderMode auth.RegistrationAdmissionMode,
	contenderMode auth.RegistrationAdmissionMode,
) (registrationResult, registrationResult) {
	t.Helper()
	resetAuthSchema(t, databaseURL)
	installAuthInsertBarrier(t, databaseURL)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	blocker, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect admission barrier blocker: %v", err)
	}
	t.Cleanup(func() { _ = blocker.Close(context.Background()) })
	if _, err := blocker.Exec(ctx, `SELECT pg_advisory_lock($1)`, authTestBarrierLockID); err != nil {
		t.Fatalf("acquire admission test barrier: %v", err)
	}
	barrierReleased := false
	t.Cleanup(func() {
		if !barrierReleased {
			_, _ = blocker.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, authTestBarrierLockID)
		}
	})

	holderStore := openNamedStore(t, databaseURL, "auth_barrier_holder")
	contenderStore := openNamedStore(t, databaseURL, "auth_barrier_contender")
	now := time.Date(2026, 7, 31, 7, 8, 9, 0, time.UTC)
	holderService := newTestAuthService(holderStore, &now, &capturingAuthMail{}, 7, holderMode, "synthetic-root")
	contenderService := newTestAuthService(contenderStore, &now, &capturingAuthMail{}, 71, contenderMode, "synthetic-root")

	holderResult := make(chan registrationResult, 1)
	go func() {
		dispatch, err := holderService.Register(ctx, syntheticEmail("barrier-holder"), "twelve-bytes", testClientMeta())
		holderResult <- registrationResult{dispatch: dispatch, err: err}
	}()
	waitForAdvisoryWait(t, databaseURL, "auth_barrier_holder")

	contenderResult := make(chan registrationResult, 1)
	go func() {
		dispatch, err := contenderService.Register(ctx, syntheticEmail("barrier-contender"), "twelve-bytes", testClientMeta())
		contenderResult <- registrationResult{dispatch: dispatch, err: err}
	}()
	waitForAdvisoryWait(t, databaseURL, "auth_barrier_contender")
	select {
	case result := <-contenderResult:
		t.Fatalf("contender returned before holder commit: attempted=%t err=%v", result.dispatch.Attempted, result.err)
	default:
	}

	if _, err := blocker.Exec(ctx, `SELECT pg_advisory_unlock($1)`, authTestBarrierLockID); err != nil {
		t.Fatalf("release admission test barrier: %v", err)
	}
	barrierReleased = true

	var holder registrationResult
	select {
	case holder = <-holderResult:
	case <-ctx.Done():
		t.Fatalf("holder did not finish after barrier release: %v", ctx.Err())
	}
	var contender registrationResult
	select {
	case contender = <-contenderResult:
	case <-ctx.Done():
		t.Fatalf("contender did not finish after holder commit: %v", ctx.Err())
	}
	return holder, contender
}

func installAuthInsertBarrier(t *testing.T, databaseURL string) {
	t.Helper()
	db := queryAuthDB(t, databaseURL)
	if _, err := db.Exec(fmt.Sprintf(`
		CREATE FUNCTION auth_test_insert_barrier() RETURNS trigger LANGUAGE plpgsql AS $barrier$
		BEGIN
			PERFORM pg_advisory_xact_lock(%d);
			RETURN NEW;
		END
		$barrier$;
		CREATE TRIGGER auth_test_insert_barrier
		BEFORE INSERT ON accounts
		FOR EACH ROW EXECUTE FUNCTION auth_test_insert_barrier()`, authTestBarrierLockID)); err != nil {
		t.Fatalf("install admission insert barrier: %v", err)
	}
}

func waitForAdvisoryWait(t *testing.T, databaseURL, applicationName string) {
	t.Helper()
	db := queryAuthDB(t, databaseURL)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var waiting bool
		if err := db.QueryRow(`SELECT EXISTS (
			SELECT 1 FROM pg_stat_activity
			WHERE application_name = $1 AND wait_event_type = 'Lock' AND wait_event = 'advisory'
		)`, applicationName).Scan(&waiting); err != nil {
			t.Fatalf("inspect admission contender wait: %v", err)
		}
		if waiting {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("contender %q never waited on the admission advisory lock", applicationName)
}

func openNamedStore(t *testing.T, databaseURL, applicationName string) *store.Store {
	t.Helper()
	namedURL := withApplicationName(t, databaseURL, applicationName)
	database, err := store.Open(context.Background(), namedURL)
	if err != nil {
		t.Fatalf("open named store %q: %v", applicationName, err)
	}
	t.Cleanup(database.Close)
	return database
}

func withApplicationName(t *testing.T, databaseURL, applicationName string) string {
	t.Helper()
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatalf("parse database URL: %v", err)
	}
	query := parsed.Query()
	query.Set("application_name", applicationName)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func newTestAuthService(
	database *store.Store,
	now *time.Time,
	mail auth.AuthMailSender,
	randomStart byte,
	mode auth.RegistrationAdmissionMode,
	rootSecret string,
) *auth.Service {
	return auth.NewService(database, auth.NewTokenIssuer(rootSecret),
		auth.WithAuthClock(func() time.Time { return *now }),
		auth.WithAuthRandom(&incrementingReader{next: randomStart}),
		auth.WithAuthMailSender(mail),
		auth.WithAttemptLimiter(database),
		auth.WithRegistrationAdmissionMode(mode),
		auth.WithPublicBaseURL("https://app.example.invalid"),
		auth.WithAuthResponseDelay(func(context.Context, time.Duration) error { return nil }),
	)
}

func resetAuthDatabase(t *testing.T, url string) *store.Store {
	t.Helper()
	resetAuthSchema(t, url)
	database, err := store.Open(context.Background(), url)
	if err != nil {
		t.Fatalf("open reset store: %v", err)
	}
	t.Cleanup(database.Close)
	return database
}

func resetAuthSchema(t *testing.T, url string) {
	t.Helper()
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatalf("open database for reset: %v", err)
	}
	if _, err := db.Exec(`DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
		_ = db.Close()
		t.Fatalf("reset database schema: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close reset database: %v", err)
	}
	if err := store.MigrateUp(url); err != nil {
		t.Fatalf("migrate reset database: %v", err)
	}
}

func queryAuthDB(t *testing.T, url string) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatalf("open auth assertion database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func assertAuthTableCounts(t *testing.T, url string, accounts, identities, actions int) {
	t.Helper()
	db := queryAuthDB(t, url)
	var gotAccounts, gotIdentities, gotActions int
	if err := db.QueryRow(`SELECT
		(SELECT count(*) FROM accounts),
		(SELECT count(*) FROM account_identities),
		(SELECT count(*) FROM auth_action_tokens)`).Scan(&gotAccounts, &gotIdentities, &gotActions); err != nil {
		t.Fatalf("read auth table counts: %v", err)
	}
	if gotAccounts != accounts || gotIdentities != identities || gotActions != actions {
		t.Fatalf("auth table counts: got=(%d,%d,%d) want=(%d,%d,%d)",
			gotAccounts, gotIdentities, gotActions, accounts, identities, actions)
	}
}

func assertStoredBearerHashes(t *testing.T, url string) {
	t.Helper()
	db := queryAuthDB(t, url)
	var badAction, badRefresh int
	if err := db.QueryRow(`SELECT
		(SELECT count(*) FROM auth_action_tokens WHERE octet_length(secret_hash) <> 32),
		(SELECT count(*) FROM refresh_session_generations WHERE octet_length(token_hash) <> 32)`).Scan(&badAction, &badRefresh); err != nil {
		t.Fatalf("inspect stored bearer hashes: %v", err)
	}
	if badAction != 0 || badRefresh != 0 {
		t.Fatalf("invalid stored bearer hash lengths: action=%d refresh=%d", badAction, badRefresh)
	}
}

func assertFamilyRevoked(t *testing.T, url, familyID string) {
	t.Helper()
	var revoked bool
	if err := queryAuthDB(t, url).QueryRow(`
		SELECT revoked_at IS NOT NULL FROM refresh_session_families WHERE id = $1`, familyID).Scan(&revoked); err != nil {
		t.Fatalf("inspect refresh family revocation: %v", err)
	}
	if !revoked {
		t.Fatal("refresh family was not revoked")
	}
}

func mustLogin(t *testing.T, service *auth.Service, email string) auth.Session {
	t.Helper()
	session, err := service.Login(context.Background(), email, "twelve-bytes", testClientMeta())
	if err != nil {
		t.Fatalf("login active account: %v", err)
	}
	return session
}

func mustRefresh(t *testing.T, service *auth.Service, wire string) auth.Session {
	t.Helper()
	session, err := service.Refresh(context.Background(), wire, testClientMeta())
	if err != nil {
		t.Fatalf("refresh active session: %v", err)
	}
	return session
}

func syntheticEmail(local string) string {
	return local + "@" + "example.invalid"
}

func testClientMeta() auth.ClientMeta {
	return auth.ClientMeta{SourceIP: netip.MustParseAddr("198.51.100.254")}
}

func actionTokenFromMail(t *testing.T, mail auth.AuthMail) string {
	t.Helper()
	parsed, err := url.Parse(mail.ActionURL)
	if err != nil {
		t.Fatalf("parse captured action URL: %v", err)
	}
	values, err := url.ParseQuery(parsed.Fragment)
	if err != nil {
		t.Fatalf("parse captured action fragment: %v", err)
	}
	token := values.Get("token")
	if token == "" {
		t.Fatal("captured action URL omitted token fragment")
	}
	return token
}

type capturingAuthMail struct {
	mu      sync.Mutex
	mails   []auth.AuthMail
	sendErr error
}

func (m *capturingAuthMail) Send(_ context.Context, mail auth.AuthMail) (auth.MailReceipt, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mails = append(m.mails, mail)
	if m.sendErr != nil {
		return auth.MailReceipt{}, m.sendErr
	}
	return auth.MailReceipt{ProviderMessageID: "synthetic-message", AcceptedAt: mail.ExpiresAt.Add(-time.Hour)}, nil
}

func (m *capturingAuthMail) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.mails)
}

func (m *capturingAuthMail) Last() auth.AuthMail {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.mails[len(m.mails)-1]
}

func (m *capturingAuthMail) SetError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sendErr = err
}

type incrementingReader struct {
	mu   sync.Mutex
	next byte
}

var _ io.Reader = (*incrementingReader)(nil)

func (r *incrementingReader) Read(buffer []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for index := range buffer {
		buffer[index] = r.next
	}
	r.next++
	return len(buffer), nil
}
