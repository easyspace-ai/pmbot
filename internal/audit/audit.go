package audit

import "polymarket-btc-bot/internal/types"

// Sink records all events for later replay/debugging.
// Implementations should be non-blocking or internally buffered.
type Sink interface {
	Write(e types.Event)
}

type NopSink struct{}

func (NopSink) Write(types.Event) {}
