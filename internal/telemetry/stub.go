package telemetry

import (
	"context"
	"time"
)

type Span struct{}

func (s *Span) End() {}

func StartProviderSpan(ctx context.Context, provider, model string) (context.Context, *Span) {
	return ctx, &Span{}
}

func RecordLatency(span *Span, start time.Time) {}

func RecordError(span *Span, err error) {}
