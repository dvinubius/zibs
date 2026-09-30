package main

import (
	"testing"
	"time"
)

func TestActiveLinksGauge(t *testing.T) {
	t.Run("reports zero for an empty store", func(t *testing.T) {
		metrics, registry := newTestMetrics(t)
		store := newTestStoreWithMetrics(t, metrics)
		if err := registerActiveLinksMetric(registry, store); err != nil {
			t.Fatalf("register active links metric: %v", err)
		}

		assertGaugeMetric(t, registry, "zibs_active_links", nil, 0)
	})

	t.Run("is collected on each scrape", func(t *testing.T) {
		metrics, registry := newTestMetrics(t)
		store := newTestStoreWithMetrics(t, metrics)
		if err := registerActiveLinksMetric(registry, store); err != nil {
			t.Fatalf("register active links metric: %v", err)
		}

		link, err := store.create("https://example.com")
		if err != nil {
			t.Fatalf("create link: %v", err)
		}
		assertGaugeMetric(t, registry, "zibs_active_links", nil, 1)

		if err := store.delete(link.Code); err != nil {
			t.Fatalf("delete link: %v", err)
		}
		assertGaugeMetric(t, registry, "zibs_active_links", nil, 0)
	})

	t.Run("excludes expired rows before cleanup", func(t *testing.T) {
		metrics, registry := newTestMetrics(t)
		store := newTestStoreWithMetrics(t, metrics)
		now := time.Date(2026, time.September, 4, 12, 0, 0, 0, time.UTC)
		store.now = func() time.Time { return now }
		if err := registerActiveLinksMetric(registry, store); err != nil {
			t.Fatalf("register active links metric: %v", err)
		}

		if _, err := store.create("https://example.com"); err != nil {
			t.Fatalf("create link: %v", err)
		}
		assertGaugeMetric(t, registry, "zibs_active_links", nil, 1)

		now = now.Add(defaultLinkTTL)
		assertGaugeMetric(t, registry, "zibs_active_links", nil, 0)
	})
}
