package core_test

import (
	"context"
	"testing"
	"time"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"golang.org/x/crypto/bcrypt"
)

func TestAuthHardeningConfigurationIsCanonical(t *testing.T) {
	manifest, err := ridu.Resolve(ridu.Config{
		Name: "Auth config", Admin: ridu.AdminConfig{User: "users"},
		Collections: []ridu.Collection{{
			Slug: "users", Auth: true,
			AuthConfig: ridu.AuthConfig{
				SessionDuration: 3 * time.Hour,
				Password: ridu.PasswordPolicy{
					MinLength: 14, MaxBytes: 64, BcryptCost: bcrypt.MinCost + 1,
				},
				MaxLoginAttempts: 7, LockDuration: 20 * time.Minute,
				PasswordReset: ridu.PasswordResetConfig{TokenDuration: 2 * time.Hour, Send: func(context.Context, ridu.PasswordResetNotification) error { return nil }},
				Verify:        &ridu.VerifyEmailConfig{TokenDuration: 48 * time.Hour, Send: func(context.Context, ridu.VerifyEmailNotification) error { return nil }},
				APIKeys:       true,
			},
			Fields: []field.Definition{field.Text("email", field.Required(), field.Unique())},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	auth := manifest.Snapshot().Collections[0].Auth
	if auth == nil || auth.SessionDurationSeconds != 10_800 || auth.PasswordMinLength != 14 ||
		auth.PasswordMaxBytes != 64 || auth.PasswordBcryptCost != bcrypt.MinCost+1 ||
		auth.MaxLoginAttempts != 7 || auth.LockDurationSeconds != 1_200 || !auth.PasswordReset ||
		auth.PasswordResetTokenDurationSeconds != 7_200 || !auth.VerifyEmail ||
		auth.VerificationTokenDurationSeconds != 172_800 || !auth.APIKeys {
		t.Fatalf("resolved auth settings = %#v", auth)
	}
}

func TestAuthHardeningDefaultsAndInvalidPolicy(t *testing.T) {
	base := ridu.Config{
		Name: "Auth config", Admin: ridu.AdminConfig{User: "users"},
		Collections: []ridu.Collection{{
			Slug: "users", Auth: true,
			Fields: []field.Definition{field.Text("email", field.Required(), field.Unique())},
		}},
	}
	manifest, err := ridu.Resolve(base)
	if err != nil {
		t.Fatal(err)
	}
	auth := manifest.Snapshot().Collections[0].Auth
	if auth.PasswordMinLength != 8 || auth.PasswordMaxBytes != 72 ||
		auth.PasswordBcryptCost != bcrypt.DefaultCost || auth.MaxLoginAttempts != 5 ||
		auth.LockDurationSeconds != 600 {
		t.Fatalf("default auth settings = %#v", auth)
	}
	base.Collections[0].AuthConfig.Password.MaxBytes = 73
	if _, err := ridu.Resolve(base); err == nil {
		t.Fatal("expected bcrypt password limit validation")
	}
	base.Collections[0].AuthConfig.Password.MaxBytes = 72
	base.Collections[0].AuthConfig.Password.BcryptCost = 17
	if _, err := ridu.Resolve(base); err == nil {
		t.Fatal("expected impractical bcrypt cost validation")
	}
}
