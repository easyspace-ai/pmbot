package signal

import (
	"math"
	"time"

	"polymarket-btc-bot/internal/types"
)

// Layer computes state variables used by the controller.
type Layer struct {
	lastP      float64
	lastTs     time.Time
	lastVel    float64
	velEWMA    float64
	accEWMA    float64
	hasInitial bool

	Alpha float64 // EWMA smoothing factor in (0,1]
}

func New() *Layer {
	return &Layer{Alpha: 0.2}
}

func (l *Layer) OnTick(t types.MarketTick) {
	p := clamp01(t.PYes)
	ts := time.Now().UTC()
	if !l.hasInitial {
		l.lastP = p
		l.lastTs = ts
		l.hasInitial = true
		return
	}
	dt := ts.Sub(l.lastTs).Seconds()
	if dt <= 0 {
		return
	}
	vel := (p - l.lastP) / dt
	acc := (vel - l.lastVel) / dt

	l.velEWMA = ewma(l.velEWMA, vel, l.Alpha)
	l.accEWMA = ewma(l.accEWMA, acc, l.Alpha)

	l.lastVel = vel
	l.lastP = p
	l.lastTs = ts
}

// Entropy returns the Shannon entropy of a Bernoulli variable with p in [0,1].
func Entropy(p float64) float64 {
	p = clamp01(p)
	if p == 0 || p == 1 {
		return 0
	}
	return -(p*math.Log(p) + (1-p)*math.Log(1-p))
}

func (l *Layer) Velocity() float64     { return l.velEWMA }
func (l *Layer) Acceleration() float64 { return l.accEWMA }

func ewma(prev, x, alpha float64) float64 {
	if alpha <= 0 {
		alpha = 0.2
	}
	if alpha > 1 {
		alpha = 1
	}
	return alpha*x + (1-alpha)*prev
}

func clamp01(x float64) float64 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}
