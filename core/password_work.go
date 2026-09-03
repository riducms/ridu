package core

import (
	"context"
	"runtime"

	operationengine "github.com/riducms/ridu/internal/operation"
	"golang.org/x/crypto/bcrypt"
)

type passwordWorkLimiter struct {
	slots chan struct{}
}

var globalPasswordWork = newPasswordWorkLimiter(passwordWorkConcurrency())

func passwordWorkConcurrency() int {
	concurrency := runtime.GOMAXPROCS(0)
	if concurrency < 2 {
		return 2
	}
	if concurrency > 8 {
		return 8
	}
	return concurrency
}

func newPasswordWorkLimiter(concurrency int) *passwordWorkLimiter {
	if concurrency < 1 {
		concurrency = 1
	}
	return &passwordWorkLimiter{slots: make(chan struct{}, concurrency)}
}

func (limiter *passwordWorkLimiter) acquire(ctx context.Context) (func(), error) {
	select {
	case limiter.slots <- struct{}{}:
		return func() { <-limiter.slots }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		return nil, &operationengine.Error{Code: "rate_limited", Status: 429, Message: "authentication capacity is temporarily exhausted; try again shortly"}
	}
}

func (application *App) generatePasswordHash(ctx context.Context, password string, cost int) ([]byte, error) {
	release, err := application.passwordWork.acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	return bcrypt.GenerateFromPassword([]byte(password), cost)
}

func (application *App) passwordMatches(ctx context.Context, hash []byte, password string) (bool, error) {
	release, err := application.passwordWork.acquire(ctx)
	if err != nil {
		return false, err
	}
	defer release()
	return bcrypt.CompareHashAndPassword(hash, []byte(password)) == nil, nil
}
