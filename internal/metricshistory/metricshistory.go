// Package metricshistory validates metric history API queries and maps storage
// buckets to JSON-friendly responses.
package metricshistory

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/masoudx/monitoring24/internal/storage"
)

// MaxRange is the longest window returned (aligned with snapshot retention policy).
const MaxRange = 31 * 24 * time.Hour

// allowedSteps are supported aggregation resolutions in seconds.
var allowedSteps = map[int64]struct{}{
	60: {}, 300: {}, 600: {}, 1800: {}, 3600: {}, 86400: {},
}

// allowedKinds maps API kind strings to storage history kinds.
var allowedKinds = map[string]struct{}{
	"cpu": {}, "ram": {}, "disk": {},
}

// Aggregator reads time-bucketed metric history from storage.
type Aggregator interface {
	QueryMetricHistoryAggregated(ctx context.Context, kind string, fromUnix, toUnix int64, step int64) ([]storage.MetricHistoryBucket, error)
}

// Query holds normalized bounds after ResolveQuery.
type Query struct {
	Kind     string
	FromUnix int64
	ToUnix   int64
	Step     int64
}

// Point is one aggregated sample for JSON APIs.
// Used and Total are averaged byte values within the bucket when kind is ram or disk.
type Point struct {
	T     int64   `json:"t"`
	V     float64 `json:"v"`
	Used  *uint64 `json:"used,omitempty"`
	Total *uint64 `json:"total,omitempty"`
}

// Response is the GET /api/metrics/history JSON body.
type Response struct {
	Kind        string  `json:"kind"`
	From        int64   `json:"from"`
	To          int64   `json:"to"`
	StepSeconds int64   `json:"step_seconds"`
	Unit        string  `json:"unit"`
	Points      []Point `json:"points"`
}

// ResolveQuery validates kind and step, clamps the window to [now−MaxRange, now], and ensures from ≤ to.
func ResolveQuery(kind string, fromUnix, toUnix, step int64, now time.Time) (Query, error) {
	if _, ok := allowedKinds[kind]; !ok {
		return Query{}, fmt.Errorf("invalid kind")
	}
	if _, ok := allowedSteps[step]; !ok {
		return Query{}, fmt.Errorf("invalid step")
	}
	nowU := now.Unix()
	if toUnix > nowU {
		toUnix = nowU
	}
	if fromUnix > toUnix {
		return Query{}, fmt.Errorf("from after to")
	}
	maxSec := int64(MaxRange.Seconds())
	if toUnix-fromUnix > maxSec {
		fromUnix = toUnix - maxSec
	}
	return Query{Kind: kind, FromUnix: fromUnix, ToUnix: toUnix, Step: step}, nil
}

// Fetch loads aggregated buckets for a resolved query.
func Fetch(ctx context.Context, db Aggregator, q Query) (Response, error) {
	buckets, err := db.QueryMetricHistoryAggregated(ctx, q.Kind, q.FromUnix, q.ToUnix, q.Step)
	if err != nil {
		return Response{}, err
	}
	pts := make([]Point, len(buckets))
	for i, b := range buckets {
		p := Point{T: b.BucketTS, V: b.Value}
		if b.AuxUsedAvg.Valid && b.AuxTotalAvg.Valid {
			u := uint64(math.Round(b.AuxUsedAvg.Float64))
			t := uint64(math.Round(b.AuxTotalAvg.Float64))
			p.Used = &u
			p.Total = &t
		}
		pts[i] = p
	}
	return Response{
		Kind:        q.Kind,
		From:        q.FromUnix,
		To:          q.ToUnix,
		StepSeconds: q.Step,
		Unit:        "percent",
		Points:      pts,
	}, nil
}
