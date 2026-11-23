package schedule

import (
	"math/rand/v2"
	"testing"
	"time"
)

func TestDefaultConfigCovers24HoursAndNeverDropsToZero(t *testing.T) {
	cfg := DefaultConfig(50.0)
	if err := cfg.Validate(); err != nil {
		t.Fatalf("DefaultConfig failed validation: %v", err)
	}

	rng := rand.New(rand.NewPCG(1234, 5678))
	base := time.Date(2026, 11, 23, 0, 0, 0, 0, time.UTC)

	var minRPS, maxRPS float64
	minRPS = 1e9
	for day := range 7 {
		for hour := range 24 {
			ts := base.Add(time.Duration(day*24+hour)*time.Hour + 30*time.Minute)
			tgt := cfg.Evaluate(ts, rng)
			if tgt.TargetRPS < cfg.MinFloorRPS {
				t.Fatalf("hour %s dropped below floor: got %v < %v", ts, tgt.TargetRPS, cfg.MinFloorRPS)
			}
			if tgt.TargetRPS > cfg.MaxCeilingRPS {
				t.Fatalf("hour %s exceeded ceiling: got %v > %v", ts, tgt.TargetRPS, cfg.MaxCeilingRPS)
			}
			if tgt.TargetRPS < minRPS {
				minRPS = tgt.TargetRPS
			}
			if tgt.TargetRPS > maxRPS {
				maxRPS = tgt.TargetRPS
			}
		}
	}
	if maxRPS <= minRPS*2.0 {
		t.Fatalf("expected realistic diurnal variation across 7 days; got min=%v max=%v", minRPS, maxRPS)
	}
}

func TestValidateRejectsGapsAndZeroFloor(t *testing.T) {
	cfg := DefaultConfig(20)
	cfg.MinFloorRPS = 0
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for zero MinFloorRPS")
	}

	cfg2 := DefaultConfig(20)
	cfg2.Windows = cfg2.Windows[:len(cfg2.Windows)-1] // omit 22:00-24:00
	if err := cfg2.Validate(); err == nil {
		t.Fatal("expected error for uncovered UTC hours")
	}
}
