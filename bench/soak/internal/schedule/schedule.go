// Package schedule implements the 7-day continuous diurnal traffic curve
// for Ruxbase W11-05 staging qualification.
package schedule

import (
	"fmt"
	"math"
	"math/rand/v2"
	"time"
)

// Window defines a daily UTC time band with sustainable capacity fraction bounds
// and a read/write mix override.
type Window struct {
	Name        string  `json:"name"`
	StartHour   int     `json:"startHour"`   // inclusive [0, 23]
	EndHour     int     `json:"endHour"`     // exclusive (1, 24]
	MinFraction float64 `json:"minFraction"` // lower fraction of pilot-qualified capacity
	MaxFraction float64 `json:"maxFraction"` // upper fraction of pilot-qualified capacity
	WriteRatio  float64 `json:"writeRatio"`  // fraction of CRUD operations that are writes
	BurstChance float64 `json:"burstChance"` // probability of short user session burst
}

// Config governs the continuous 7-day load profile.
type Config struct {
	// CapacityRPS is the sustainable capacity derived from the 60-minute pilot,
	// never an arbitrary peak throughput figure.
	CapacityRPS float64 `json:"capacityRps"`
	// MinFloorRPS guarantees offered traffic never intentionally drops to zero.
	MinFloorRPS float64 `json:"minFloorRps"`
	// MaxCeilingRPS caps burst spikes within approved VPS safety headroom.
	MaxCeilingRPS float64 `json:"maxCeilingRps"`
	// JitterFraction adds +/- randomized arrival jitter (e.g. 0.12 = +/-12%).
	JitterFraction float64 `json:"jitterFraction"`
	// WeekendMultiplier slightly alters Saturday/Sunday traffic shape while keeping
	// realistic sustained customer writes.
	WeekendMultiplier float64 `json:"weekendMultiplier"`
	// Windows defines the 24-hour UTC diurnal curve.
	Windows []Window `json:"windows"`
}

// DefaultConfig returns the Phase 3 W11-05 diurnal schedule scaled to capacityRPS.
func DefaultConfig(capacityRPS float64) Config {
	if capacityRPS <= 0 {
		capacityRPS = 40.0
	}
	floor := math.Max(1.0, capacityRPS*0.10)
	ceiling := capacityRPS * 0.90
	return Config{
		CapacityRPS:       capacityRPS,
		MinFloorRPS:       floor,
		MaxCeilingRPS:     ceiling,
		JitterFraction:    0.10,
		WeekendMultiplier: 0.85,
		Windows: []Window{
			{Name: "overnight_trough", StartHour: 0, EndHour: 6, MinFraction: 0.10, MaxFraction: 0.25, WriteRatio: 0.20, BurstChance: 0.05},
			{Name: "morning_ramp", StartHour: 6, EndHour: 9, MinFraction: 0.25, MaxFraction: 0.45, WriteRatio: 0.25, BurstChance: 0.10},
			{Name: "midday_climb", StartHour: 9, EndHour: 12, MinFraction: 0.45, MaxFraction: 0.65, WriteRatio: 0.30, BurstChance: 0.15},
			{Name: "afternoon_peak_1", StartHour: 12, EndHour: 15, MinFraction: 0.60, MaxFraction: 0.75, WriteRatio: 0.30, BurstChance: 0.18},
			{Name: "late_afternoon", StartHour: 15, EndHour: 18, MinFraction: 0.40, MaxFraction: 0.60, WriteRatio: 0.28, BurstChance: 0.12},
			{Name: "evening_peak", StartHour: 18, EndHour: 22, MinFraction: 0.70, MaxFraction: 0.85, WriteRatio: 0.32, BurstChance: 0.22},
			{Name: "late_night_ramp_down", StartHour: 22, EndHour: 24, MinFraction: 0.20, MaxFraction: 0.40, WriteRatio: 0.22, BurstChance: 0.06},
		},
	}
}

// Validate checks that the schedule covers all 24 hours without gaps and never drops to zero.
func (c Config) Validate() error {
	if c.CapacityRPS <= 0 {
		return fmt.Errorf("capacityRps must be > 0 (got %v)", c.CapacityRPS)
	}
	if c.MinFloorRPS <= 0 {
		return fmt.Errorf("minFloorRps must be > 0 to prevent zero-traffic gaps (got %v)", c.MinFloorRPS)
	}
	if c.MaxCeilingRPS < c.MinFloorRPS {
		return fmt.Errorf("maxCeilingRps (%v) must be >= minFloorRps (%v)", c.MaxCeilingRPS, c.MinFloorRPS)
	}
	covered := make([]bool, 24)
	for _, w := range c.Windows {
		if w.StartHour < 0 || w.EndHour > 24 || w.StartHour >= w.EndHour {
			return fmt.Errorf("invalid window %q hours [%d, %d)", w.Name, w.StartHour, w.EndHour)
		}
		if w.MinFraction <= 0 || w.MaxFraction < w.MinFraction || w.MaxFraction > 1.0 {
			return fmt.Errorf("invalid window %q fractions [%v, %v]", w.Name, w.MinFraction, w.MaxFraction)
		}
		for h := w.StartHour; h < w.EndHour; h++ {
			covered[h] = true
		}
	}
	for h, ok := range covered {
		if !ok {
			return fmt.Errorf("hour %02d UTC is not covered by any traffic window", h)
		}
	}
	return nil
}

// Target represents the resolved workload target for a point in time.
type Target struct {
	WindowName string  `json:"windowName"`
	TargetRPS  float64 `json:"targetRps"`
	WriteRatio float64 `json:"writeRatio"`
	Burst      bool    `json:"burst"`
}

// Evaluate computes the target arrival rate and write mix for timestamp t (UTC).
// If rng is non-nil, bounded randomized jitter and occasional session bursts are applied.
func (c Config) Evaluate(t time.Time, rng *rand.Rand) Target {
	utc := t.UTC()
	hour := utc.Hour()
	minuteFrac := (float64(utc.Minute()) + float64(utc.Second())/60.0) / 60.0

	win := c.Windows[0]
	for _, w := range c.Windows {
		if hour >= w.StartHour && hour < w.EndHour {
			win = w
			break
		}
	}

	// Smooth progress within the window using a sine wave to avoid artificial step cliffs.
	span := float64(win.EndHour - win.StartHour)
	pos := (float64(hour-win.StartHour) + minuteFrac) / math.Max(span, 1.0)
	wave := 0.5 - 0.5*math.Cos(math.Pi*pos)
	frac := win.MinFraction + (win.MaxFraction-win.MinFraction)*wave

	// Day-of-week modulation: slight weekend shape variation while preserving sustained load.
	dayMult := 1.0
	if utc.Weekday() == time.Saturday || utc.Weekday() == time.Sunday {
		if c.WeekendMultiplier > 0 && c.WeekendMultiplier <= 1.0 {
			dayMult = c.WeekendMultiplier
		}
	}

	rps := c.CapacityRPS * frac * dayMult
	burst := false

	if rng != nil {
		if c.JitterFraction > 0 {
			jitter := 1.0 + (rng.Float64()*2.0-1.0)*c.JitterFraction
			rps *= jitter
		}
		if win.BurstChance > 0 && rng.Float64() < win.BurstChance {
			burst = true
			rps *= 1.15
		}
	}

	if rps < c.MinFloorRPS {
		rps = c.MinFloorRPS
	}
	if c.MaxCeilingRPS > 0 && rps > c.MaxCeilingRPS {
		rps = c.MaxCeilingRPS
	}

	return Target{
		WindowName: win.Name,
		TargetRPS:  math.Round(rps*100) / 100,
		WriteRatio: win.WriteRatio,
		Burst:      burst,
	}
}
