package core

import (
	"context"
	"errors"
	"testing"

	"github.com/riducms/ridu/field"
	operationengine "github.com/riducms/ridu/internal/operation"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/store"
	"golang.org/x/crypto/bcrypt"
)

func TestLoginRejectsBeforeBcryptWhenPasswordWorkIsSaturated(t *testing.T) {
	application, err := New(Config{
		Name:  "Password work admission",
		Admin: AdminConfig{User: "users"},
		Collections: []Collection{{
			Slug: "users", Auth: true,
			AuthConfig: AuthConfig{Password: PasswordPolicy{BcryptCost: bcrypt.MinCost}},
			Fields:     field.Fields{field.Email("email").Required().Unique()},
		}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	limiter := newPasswordWorkLimiter(1)
	limiter.slots <- struct{}{}
	application.passwordWork = limiter
	_, err = application.Login(context.Background(), "users", "rotated@example.test", "password")
	var operationError *operationengine.Error
	if !errors.As(err, &operationError) || operationError.Code != "rate_limited" || operationError.Status != 429 {
		t.Fatalf("saturated password work error = %#v, %v", operationError, err)
	}
}

func TestInitializedAnonymousAuthCreationRejectsBeforePasswordWork(t *testing.T) {
	application, err := New(Config{
		Name: "Initialized auth admission", Admin: AdminConfig{User: "users"},
		Collections: []Collection{{
			Slug: "users", Auth: true,
			AuthConfig: AuthConfig{Password: PasswordPolicy{BcryptCost: bcrypt.MinCost}},
			Fields:     field.Fields{field.Email("email").Required().Unique()},
		}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.CreateAuthUserForTransport(context.Background(), "users", store.Values{"email": store.String("first@example.test")}, "correct-horse", MutationOptions{}); err != nil {
		t.Fatal(err)
	}
	limiter := newPasswordWorkLimiter(1)
	limiter.slots <- struct{}{}
	application.passwordWork = limiter
	_, err = application.CreateAuthUserForTransport(context.Background(), "users", store.Values{"email": store.String("second@example.test")}, "correct-horse", MutationOptions{})
	var operationError *operationengine.Error
	if !errors.As(err, &operationError) || operationError.Code != "access_denied" || operationError.Status != 403 {
		t.Fatalf("initialized auth create error = %#v, %v", operationError, err)
	}
}
