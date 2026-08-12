package transport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"
)

type HTTPDoer interface {
	Do(request *http.Request) (*http.Response, error)
}

type ResilientClient struct {
	client      HTTPDoer
	health      *HealthState
	maxAttempts int
	maxBackoff  time.Duration
	now         func() time.Time
	sleep       func(context.Context, time.Duration) error
}

func NewResilientClient(client HTTPDoer, health *HealthState) *ResilientClient {
	return &ResilientClient{
		client:      client,
		health:      health,
		maxAttempts: 5,
		maxBackoff:  60 * time.Second,
		now:         time.Now,
		sleep:       sleepContext,
	}
}

func sleepContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func retryableStatus(status int) bool {
	return status == http.StatusTooManyRequests || status >= 500
}

func retryableError(ctx context.Context, err error) bool {
	if err == nil || ctx.Err() != nil || errors.Is(err, context.Canceled) {
		return false
	}
	var networkError net.Error
	return errors.As(err, &networkError)
}

func (c *ResilientClient) backoff(attempt int, response *http.Response) time.Duration {
	if response != nil {
		if seconds, err := strconv.Atoi(response.Header.Get("Retry-After")); err == nil && seconds > 0 {
			duration := time.Duration(seconds) * time.Second
			if duration < c.maxBackoff {
				return duration
			}
			return c.maxBackoff
		}
	}
	duration := time.Second << (attempt - 1)
	if duration > c.maxBackoff {
		return c.maxBackoff
	}
	return duration
}

func cloneRequest(request *http.Request, ctx context.Context, attempt int) (*http.Request, error) {
	cloned := request.Clone(ctx)
	if request.Body == nil {
		return cloned, nil
	}
	if attempt == 1 {
		return cloned, nil
	}
	if request.GetBody == nil {
		return nil, errors.New("request body cannot be replayed for retry")
	}
	body, err := request.GetBody()
	if err != nil {
		return nil, fmt.Errorf("recreate request body: %w", err)
	}
	cloned.Body = body
	return cloned, nil
}

func (c *ResilientClient) Do(request *http.Request) (*http.Response, error) {
	ctx := request.Context()
	var lastErr error
	for attempt := 1; attempt <= c.maxAttempts; attempt++ {
		current, err := cloneRequest(request, ctx, attempt)
		if err != nil {
			c.health.Exhausted(err)
			return nil, err
		}
		response, err := c.client.Do(current)
		if err == nil && !retryableStatus(response.StatusCode) {
			c.health.Success()
			return response, nil
		}

		if err != nil {
			lastErr = err
			if !retryableError(ctx, err) {
				c.health.Exhausted(err)
				return nil, err
			}
		} else {
			lastErr = fmt.Errorf("HTTP %s", response.Status)
			if attempt == c.maxAttempts {
				c.health.Exhausted(lastErr)
				return response, nil
			}
			_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
			response.Body.Close()
		}

		if attempt == c.maxAttempts {
			c.health.Exhausted(lastErr)
			return nil, lastErr
		}
		delay := c.backoff(attempt, response)
		nextRetry := c.now().Add(delay)
		c.health.Failure(lastErr, nextRetry, attempt+1)
		if err := c.sleep(ctx, delay); err != nil {
			c.health.Exhausted(err)
			return nil, err
		}
	}
	return nil, lastErr
}
