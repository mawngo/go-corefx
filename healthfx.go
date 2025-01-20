package corefx

import (
	"context"
	"errors"
	"go.uber.org/fx"
	"sync/atomic"
)

// ErrUnavailable indicate that the service is busy/overload, thus not able to process new traffic.
// For other cases like connection failure, deadlock, ... the service should return more specific error instead.
var ErrUnavailable = errors.New("err unavailable")

const (
	HealthStatusUp    = "UP"
	HealthStatusDown  = "DOWN"
	HealthUnavailable = "UNAVAILABLE"
)

// HealthIndicator an empty interface for registering type for health probing.
type HealthIndicator interface {
	Readiness(ctx context.Context) error
	Liveness(ctx context.Context) error
}

// ReadinessThresholder configure maximum number of continuously not-ready probes before this instance considered failure.
// By default, this value is -1, meaning that the readiness status should not affect liveness status.
type ReadinessThresholder interface {
	ReadinessThreshold() int
}

type HealthStatus struct {
	Status string `json:"status"`
	Err    error  `json:"-"`
}

// Health report the combined readiness/liveness of all HealthIndicator.
type Health struct {
	checkers          []HealthIndicator
	maxNotReadyConfig map[HealthIndicator]int
	notReadyCount     map[HealthIndicator]*atomic.Int32
}

type AvailabilityProbeParams struct {
	fx.In
	Checkers []HealthIndicator `group:"health_indicator"`
}

// NewHealth create new Health instance.
func NewHealth(p AvailabilityProbeParams) *Health {
	probe := Health{
		checkers: p.Checkers,
	}

	thresholdedReadinessReporters := make([]HealthIndicator, 0, len(p.Checkers))
	for _, s := range p.Checkers {
		if thresholder, ok := s.(ReadinessThresholder); ok {
			if thresholder.ReadinessThreshold() >= 0 {
				thresholdedReadinessReporters = append(thresholdedReadinessReporters, s)
			}
		}
	}

	// Prepare readiness thresholds.
	probe.maxNotReadyConfig = make(map[HealthIndicator]int, len(thresholdedReadinessReporters))
	probe.notReadyCount = make(map[HealthIndicator]*atomic.Int32, len(thresholdedReadinessReporters))
	for i := range thresholdedReadinessReporters {
		probe.maxNotReadyConfig[thresholdedReadinessReporters[i]] = thresholdedReadinessReporters[i].(ReadinessThresholder).ReadinessThreshold()
		probe.notReadyCount[thresholdedReadinessReporters[i]] = &atomic.Int32{}
	}
	return &probe
}

func (p *Health) Liveness(ctx context.Context) (bool, map[HealthIndicator]HealthStatus) {
	liveness := true
	res := make(map[HealthIndicator]HealthStatus, len(p.checkers))
	for _, l := range p.checkers {
		err := l.Liveness(ctx)
		if err != nil {
			liveness = false
			res[l] = HealthStatus{
				Status: HealthStatusDown,
				Err:    err,
			}
			continue
		}

		if threshold, ok := p.maxNotReadyConfig[l]; ok {
			notReadyCnt := p.notReadyCount[l].Load()
			if notReadyCnt > int32(threshold) {
				liveness = false
				res[l] = HealthStatus{
					Status: HealthStatusDown,
					Err:    ErrUnavailable,
				}
				continue
			}
		}

		res[l] = HealthStatus{
			Status: HealthStatusUp,
		}
		continue
	}
	return liveness, res
}

func (p *Health) Readiness(ctx context.Context) (bool, map[HealthIndicator]HealthStatus) {
	readiness := true
	res := make(map[HealthIndicator]HealthStatus, len(p.checkers))
	for _, r := range p.checkers {
		err := r.Readiness(ctx)
		if err == nil {
			res[r] = HealthStatus{
				Status: HealthStatusUp,
			}
			if cnt, ok := p.notReadyCount[r]; ok {
				cnt.Store(0)
			}
			continue
		}

		readiness = false
		res[r] = HealthStatus{
			Status: HealthUnavailable,
			Err:    err,
		}
		if cnt, ok := p.notReadyCount[r]; ok {
			cnt.Add(1)
		}
		continue
	}
	return readiness, res
}

// AsIndicator register function into HealthIndicator.
func AsIndicator(f any) any {
	return fx.Annotate(
		f,
		fx.As(new(HealthIndicator)),
		fx.ResultTags(`group:"health_indicator"`),
	)
}
