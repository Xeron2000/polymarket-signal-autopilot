package runtime

import (
	"context"
	"fmt"
	"time"

	"polymarket-signal/internal/connectors/polymarket"
	"polymarket-signal/internal/ingestion"
)

type MarketStream interface {
	Stream(ctx context.Context, assetIDs []string) (<-chan polymarket.WSMarketUpdate, <-chan error)
}

type RawEventSink interface {
	Write(event ingestion.RawEvent) (bool, error)
}

type Worker struct {
	stream MarketStream
	sink   RawEventSink
}

func NewWorker(stream MarketStream, sink RawEventSink) *Worker {
	return &Worker{stream: stream, sink: sink}
}

func (w *Worker) Run(ctx context.Context, assetIDs []string) error {
	if len(assetIDs) == 0 {
		return fmt.Errorf("assetIDs are required")
	}

	updates, errs := w.stream.Stream(ctx, assetIDs)

	for {
		select {
		case <-ctx.Done():
			return nil
		case err, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}
			if err != nil {
				if ctx.Err() != nil {
					return nil
				}
				continue
			}
		case update, ok := <-updates:
			if !ok {
				return nil
			}

			now := time.Now().UTC()
			event := ingestion.RawEvent{
				ID:           fmt.Sprintf("%s-%s-%d", update.AssetID, update.Type, now.UnixNano()),
				Source:       "polymarket-ws",
				Payload:      append([]byte(nil), update.Payload...),
				SourceTS:     now,
				IngestTS:     now,
				NormalizedTS: now,
			}
			if _, err := w.sink.Write(event); err != nil {
				return fmt.Errorf("persist ws event: %w", err)
			}
		}
	}
}
