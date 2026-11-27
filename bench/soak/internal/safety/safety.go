// Package safety implements fail-closed emergency stop guardrails and
// shared-production isolation preflight checks for W11-05 staging soak campaigns.
package safety

import (
	"fmt"
	"strings"
)

// Thresholds defines hard operational stop limits to protect staging integrity
// and prevent unbounded disk/WAL exhaustion.
type Thresholds struct {
	MaxCrossTenantLeaks        int64   `json:"maxCrossTenantLeaks"`
	MaxStorageChecksumErrors   int64   `json:"maxStorageChecksumErrors"`
	MaxCommittedDataLoss       int64   `json:"maxCommittedDataLoss"`
	MinDiskFreePct             float64 `json:"minDiskFreePct"`
	MaxRetainedWALBytes        int64   `json:"maxRetainedWalBytes"`
	MaxConsecutiveUnavailTicks int     `json:"maxConsecutiveUnavailTicks"`
	MaxSustainedErrorRatePct   float64 `json:"maxSustainedErrorRatePct"`
}

// DefaultThresholds returns conservative fail-closed safety bounds.
func DefaultThresholds() Thresholds {
	return Thresholds{
		MaxCrossTenantLeaks:        0,
		MaxStorageChecksumErrors:   0,
		MaxCommittedDataLoss:       0,
		MinDiskFreePct:             15.0,
		MaxRetainedWALBytes:        900 * 1024 * 1024, // 900 MiB (below 1 GiB max_slot_wal_keep_size)
		MaxConsecutiveUnavailTicks: 5,                 // 5 consecutive 60s samples (5m)
		MaxSustainedErrorRatePct:   25.0,
	}
}

// Observation summarizes the current live system state evaluated by the safety controller.
type Observation struct {
	CrossTenantLeaks       int64
	StorageChecksumErrors  int64
	CommittedDataLoss      int64
	DiskFreePct            float64
	RetainedWALBytes       int64
	ConsecutiveUnavailTick int
	WindowErrorRatePct     float64
	WindowTotalOps         int64
	ControlledFaultActive  bool
}

// Decision captures whether the campaign may continue or must halt immediately.
type Decision struct {
	Stop     bool   `json:"stop"`
	Code     string `json:"code,omitempty"`
	Severity string `json:"severity,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// Evaluate checks hard stop conditions. Integrity and tenant-isolation breaches
// ALWAYS stop immediately, even during a controlled fault window.
func Evaluate(th Thresholds, obs Observation) Decision {
	if obs.CrossTenantLeaks > th.MaxCrossTenantLeaks {
		return Decision{
			Stop:     true,
			Code:     "cross_tenant_leak",
			Severity: "critical",
			Reason:   fmt.Sprintf("CRITICAL: %d unauthorized cross-tenant exposure(s) detected", obs.CrossTenantLeaks),
		}
	}
	if obs.StorageChecksumErrors > th.MaxStorageChecksumErrors {
		return Decision{
			Stop:     true,
			Code:     "storage_checksum_mismatch",
			Severity: "critical",
			Reason:   fmt.Sprintf("CRITICAL: %d storage SHA-256 checksum violation(s) detected", obs.StorageChecksumErrors),
		}
	}
	if obs.CommittedDataLoss > th.MaxCommittedDataLoss {
		return Decision{
			Stop:     true,
			Code:     "committed_data_loss",
			Severity: "critical",
			Reason:   fmt.Sprintf("CRITICAL: %d committed record(s)/job(s) missing from PostgreSQL", obs.CommittedDataLoss),
		}
	}
	if obs.DiskFreePct > 0 && obs.DiskFreePct < th.MinDiskFreePct {
		return Decision{
			Stop:     true,
			Code:     "disk_exhaustion_risk",
			Severity: "critical",
			Reason:   fmt.Sprintf("CRITICAL: free disk %.2f%% below hard safety floor %.2f%%", obs.DiskFreePct, th.MinDiskFreePct),
		}
	}
	if th.MaxRetainedWALBytes > 0 && obs.RetainedWALBytes > th.MaxRetainedWALBytes {
		return Decision{
			Stop:     true,
			Code:     "unbounded_wal_growth",
			Severity: "critical",
			Reason:   fmt.Sprintf("CRITICAL: retained WAL %d bytes exceeds hard safety cap %d bytes", obs.RetainedWALBytes, th.MaxRetainedWALBytes),
		}
	}
	if !obs.ControlledFaultActive {
		if th.MaxConsecutiveUnavailTicks > 0 && obs.ConsecutiveUnavailTick >= th.MaxConsecutiveUnavailTicks {
			return Decision{
				Stop:     true,
				Code:     "sustained_unavailability",
				Severity: "critical",
				Reason:   fmt.Sprintf("CRITICAL: application unavailable for %d consecutive telemetry intervals", obs.ConsecutiveUnavailTick),
			}
		}
		if obs.WindowTotalOps >= 100 && th.MaxSustainedErrorRatePct > 0 && obs.WindowErrorRatePct >= th.MaxSustainedErrorRatePct {
			return Decision{
				Stop:     true,
				Code:     "sustained_error_spike",
				Severity: "critical",
				Reason:   fmt.Sprintf("CRITICAL: window error rate %.2f%% >= %.2f%% (%d ops)", obs.WindowErrorRatePct, th.MaxSustainedErrorRatePct, obs.WindowTotalOps),
			}
		}
	}
	return Decision{Stop: false}
}

// CheckHostIsolation inspects running container names and refuses launch if
// unrelated production containers are detected on the host.
func CheckHostIsolation(containerNames []string, allowedPrefix string) error {
	if allowedPrefix == "" {
		allowedPrefix = "ruxbase-soak-"
	}
	var prodIndicators []string
	var unrelated []string
	for _, raw := range containerNames {
		name := strings.TrimSpace(raw)
		if name == "" || strings.HasPrefix(name, allowedPrefix) {
			continue
		}
		unrelated = append(unrelated, name)
		low := strings.ToLower(name)
		if strings.Contains(low, "prod") || strings.Contains(low, "production") || strings.Contains(low, "live") {
			prodIndicators = append(prodIndicators, name)
		}
	}
	if len(prodIndicators) > 0 {
		return fmt.Errorf("ISOLATION BLOCKER: shared production containers detected on host (%s); W11-05 soak must run on a dedicated staging VPS", strings.Join(prodIndicators, ", "))
	}
	if len(unrelated) > 0 {
		return fmt.Errorf("ISOLATION WARNING: unrelated containers running on host (%s); dedicated staging host required", strings.Join(unrelated, ", "))
	}
	return nil
}
