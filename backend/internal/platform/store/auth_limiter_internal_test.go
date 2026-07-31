package store

import (
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
)

func TestAuthAttemptBudgetMatrix(t *testing.T) {
	tests := map[auth.AuthAction]authAttemptBudget{
		auth.AuthActionLogin:              {subjectLimit: 5, sourceLimit: 30, window: 15 * time.Minute},
		auth.AuthActionChangePassword:     {subjectLimit: 5, sourceLimit: 30, window: 15 * time.Minute},
		auth.AuthActionRegister:           {subjectLimit: 3, sourceLimit: 20, window: time.Hour},
		auth.AuthActionResendVerification: {subjectLimit: 3, sourceLimit: 20, window: time.Hour},
		auth.AuthActionForgotPassword:     {subjectLimit: 3, sourceLimit: 20, window: time.Hour},
		auth.AuthActionVerifyToken:        {subjectLimit: 10, sourceLimit: 30, window: 15 * time.Minute},
		auth.AuthActionResetToken:         {subjectLimit: 10, sourceLimit: 30, window: 15 * time.Minute},
	}
	for action, want := range tests {
		got, err := authAttemptBudgetFor(action)
		if err != nil || got != want {
			t.Fatalf("budget %q = %#v, err=%v; want %#v", action, got, err, want)
		}
	}
	if _, err := authAttemptBudgetFor(auth.AuthAction("unknown")); err == nil {
		t.Fatal("unknown auth action must fail closed")
	}
}
