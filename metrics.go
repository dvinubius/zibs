package main

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type metrics struct {
	httpRequests          *prometheus.CounterVec
	httpRequestDuration   *prometheus.HistogramVec
	linkOperations        *prometheus.CounterVec
	dbOperationDuration   *prometheus.HistogramVec
	expiredLinksDeleted   prometheus.Counter
	expiryCleanupDuration *prometheus.HistogramVec
}

func newMetricsHandler(registry *prometheus.Registry) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
	return mux
}

func newMetrics() (*metrics, *prometheus.Registry, error) {
	registry := prometheus.NewRegistry()
	if err := registry.Register(collectors.NewGoCollector()); err != nil {
		return nil, nil, fmt.Errorf("register Go runtime collector: %w", err)
	}
	if err := registry.Register(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{})); err != nil {
		return nil, nil, fmt.Errorf("register process collector: %w", err)
	}

	httpRequests := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "zibs_http_requests_total",
			Help: "Total number of completed HTTP requests.",
		},
		[]string{"route", "method", "status"},
	)

	httpRequestDuration := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "zibs_http_request_duration_seconds",
			Help:    "HTTP request duration in seconds.",
			Buckets: []float64{0.0005, 0.001, 0.002, 0.003, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2},
		},
		[]string{"route", "method"},
	)

	linkOperations := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "zibs_link_operations_total",
			Help: "Total number of link operations.",
		},
		[]string{"operation", "result"},
	)

	dbOperationDuration := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "zibs_db_operation_duration_seconds",
			Help:    "DB operation duration in seconds.",
			Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2},
		},
		[]string{"operation", "result"},
	)

	expiredLinksDeleted := prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "zibs_expired_links_deleted_total",
			Help: "Total number of expired link deletions.",
		},
	)

	expiryCleanupDuration := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "zibs_expiry_cleanup_duration_seconds",
			Help:    "Expiry cleanup duration in seconds.",
			Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2},
		},
		[]string{"result"},
	)

	if err := registry.Register(httpRequests); err != nil {
		return nil, nil, fmt.Errorf("register HTTP request counter: %w", err)
	}

	if err := registry.Register(httpRequestDuration); err != nil {
		return nil, nil, fmt.Errorf("register HTTP request duration histogram: %w", err)
	}

	if err := registry.Register(linkOperations); err != nil {
		return nil, nil, fmt.Errorf("register Link Operations counter: %w", err)
	}

	if err := registry.Register(dbOperationDuration); err != nil {
		return nil, nil, fmt.Errorf("register db operation duration histogram: %w", err)
	}

	if err := registry.Register(expiredLinksDeleted); err != nil {
		return nil, nil, fmt.Errorf("register expiry links deleted counter: %w", err)
	}

	if err := registry.Register(expiryCleanupDuration); err != nil {
		return nil, nil, fmt.Errorf("register expiry cleanup duration histogram: %w", err)
	}

	return &metrics{
		httpRequests:          httpRequests,
		httpRequestDuration:   httpRequestDuration,
		linkOperations:        linkOperations,
		dbOperationDuration:   dbOperationDuration,
		expiredLinksDeleted:   expiredLinksDeleted,
		expiryCleanupDuration: expiryCleanupDuration,
	}, registry, nil
}

func registerDBStatsMetrics(registry *prometheus.Registry, db *sql.DB) error {
	collectors := []prometheus.Collector{
		prometheus.NewGaugeFunc(
			prometheus.GaugeOpts{
				Name: "zibs_db_open_connections",
				Help: "Current number of open database connections.",
			},
			func() float64 { return float64(db.Stats().OpenConnections) },
		),
		prometheus.NewGaugeFunc(
			prometheus.GaugeOpts{
				Name: "zibs_db_in_use_connections",
				Help: "Current number of database connections in use.",
			},
			func() float64 { return float64(db.Stats().InUse) },
		),
		prometheus.NewGaugeFunc(
			prometheus.GaugeOpts{
				Name: "zibs_db_idle_connections",
				Help: "Current number of idle database connections.",
			},
			func() float64 { return float64(db.Stats().Idle) },
		),
		prometheus.NewCounterFunc(
			prometheus.CounterOpts{
				Name: "zibs_db_wait_count_total",
				Help: "Total number of waits for a database connection.",
			},
			func() float64 { return float64(db.Stats().WaitCount) },
		),
		prometheus.NewCounterFunc(
			prometheus.CounterOpts{
				Name: "zibs_db_wait_duration_seconds_total",
				Help: "Total time spent waiting for a database connection in seconds.",
			},
			func() float64 { return db.Stats().WaitDuration.Seconds() },
		),
	}

	for _, collector := range collectors {
		if err := registry.Register(collector); err != nil {
			return fmt.Errorf("register database stats collector: %w", err)
		}
	}

	return nil
}

// registerActiveLinksMetric registers a scrape-time count of links which have
// not reached their expiry time. Expired rows are excluded even before the
// periodic cleanup removes them.
func registerActiveLinksMetric(registry *prometheus.Registry, store *linkStore) error {
	return registry.Register(prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Name: "zibs_active_links",
			Help: "Current number of links that have not expired.",
		},
		func() float64 {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()

			count, err := store.activeLinkCount(ctx)
			if err != nil {
				return math.NaN()
			}
			return float64(count)
		},
	))
}
