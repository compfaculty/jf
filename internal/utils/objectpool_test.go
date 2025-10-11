package utils

import (
	"jf/internal/models"
	"sync"
	"testing"
)

func TestObjectPool(t *testing.T) {
	pool := &ObjectPool{
		jobPool: sync.Pool{
			New: func() interface{} {
				return &models.Job{}
			},
		},
		scrapedJobPool: sync.Pool{
			New: func() interface{} {
				return &models.ScrapedJob{}
			},
		},
		anchorPool: sync.Pool{
			New: func() interface{} {
				return &Anchor{}
			},
		},
	}

	// Test Job pooling
	t.Run("Job Pool", func(t *testing.T) {
		job1 := pool.GetJob()
		job2 := pool.GetJob()

		if job1 == job2 {
			t.Error("Expected different job instances")
		}

		// Set some data
		job1.ID = "test-id"
		job1.Title = "test title"

		pool.PutJob(job1)

		// Get job again - should be reset
		job3 := pool.GetJob()
		if job3.ID != "" || job3.Title != "" {
			t.Error("Expected job to be reset after PutJob")
		}

		pool.PutJob(job2)
		pool.PutJob(job3)
	})

	// Test ScrapedJob pooling
	t.Run("ScrapedJob Pool", func(t *testing.T) {
		job1 := pool.GetScrapedJob()
		job2 := pool.GetScrapedJob()

		if job1 == job2 {
			t.Error("Expected different scraped job instances")
		}

		// Set some data
		job1.Title = "test title"
		job1.URL = "http://example.com"

		pool.PutScrapedJob(job1)

		// Get job again - should be reset
		job3 := pool.GetScrapedJob()
		if job3.Title != "" || job3.URL != "" {
			t.Error("Expected scraped job to be reset after PutScrapedJob")
		}

		pool.PutScrapedJob(job2)
		pool.PutScrapedJob(job3)
	})

	// Test Anchor pooling
	t.Run("Anchor Pool", func(t *testing.T) {
		anchor1 := pool.GetAnchor()
		anchor2 := pool.GetAnchor()

		if anchor1 == anchor2 {
			t.Error("Expected different anchor instances")
		}

		// Set some data
		anchor1.Text = "test text"
		anchor1.Href = "http://example.com"

		pool.PutAnchor(anchor1)

		// Get anchor again - should be reset
		anchor3 := pool.GetAnchor()
		if anchor3.Text != "" || anchor3.Href != "" {
			t.Error("Expected anchor to be reset after PutAnchor")
		}

		pool.PutAnchor(anchor2)
		pool.PutAnchor(anchor3)
	})
}

func TestObjectPoolConcurrency(t *testing.T) {
	const numGoroutines = 50
	const numOperations = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				// Test Job pool
				job := GetJob()
				job.ID = "test-id"
				job.Title = "test title"
				PutJob(job)

				// Test ScrapedJob pool
				scrapedJob := GetScrapedJob()
				scrapedJob.Title = "scraped title"
				scrapedJob.URL = "http://example.com"
				PutScrapedJob(scrapedJob)

				// Test Anchor pool
				anchor := GetAnchor()
				anchor.Text = "anchor text"
				anchor.Href = "http://example.com"
				PutAnchor(anchor)
			}
		}()
	}

	wg.Wait()
	// If we get here without race conditions, the test passes
}

func TestSlicePool(t *testing.T) {
	pool := &SlicePool{
		jobSlicePool: sync.Pool{
			New: func() interface{} {
				return make([]models.Job, 0, 16)
			},
		},
		scrapedJobSlicePool: sync.Pool{
			New: func() interface{} {
				return make([]models.ScrapedJob, 0, 16)
			},
		},
		stringSlicePool: sync.Pool{
			New: func() interface{} {
				return make([]string, 0, 16)
			},
		},
	}

	// Test Job slice pooling
	t.Run("Job Slice Pool", func(t *testing.T) {
		slice1 := pool.GetJobSlice()
		if cap(slice1) < 16 {
			t.Errorf("Expected capacity >= 16, got %d", cap(slice1))
		}
		if len(slice1) != 0 {
			t.Errorf("Expected length 0, got %d", len(slice1))
		}

		// Add some items
		slice1 = append(slice1, models.Job{ID: "1"}, models.Job{ID: "2"})
		pool.PutJobSlice(slice1)

		// Get slice again - should be cleared
		slice2 := pool.GetJobSlice()
		if len(slice2) != 0 {
			t.Errorf("Expected cleared slice, got length %d", len(slice2))
		}
	})

	// Test ScrapedJob slice pooling
	t.Run("ScrapedJob Slice Pool", func(t *testing.T) {
		slice1 := pool.GetScrapedJobSlice()
		if cap(slice1) < 16 {
			t.Errorf("Expected capacity >= 16, got %d", cap(slice1))
		}
		if len(slice1) != 0 {
			t.Errorf("Expected length 0, got %d", len(slice1))
		}

		// Add some items
		slice1 = append(slice1, models.ScrapedJob{Title: "test"}, models.ScrapedJob{Title: "test2"})
		pool.PutScrapedJobSlice(slice1)

		// Get slice again - should be cleared
		slice2 := pool.GetScrapedJobSlice()
		if len(slice2) != 0 {
			t.Errorf("Expected cleared slice, got length %d", len(slice2))
		}
	})

	// Test string slice pooling
	t.Run("String Slice Pool", func(t *testing.T) {
		slice1 := pool.GetStringSlice()
		if cap(slice1) < 16 {
			t.Errorf("Expected capacity >= 16, got %d", cap(slice1))
		}
		if len(slice1) != 0 {
			t.Errorf("Expected length 0, got %d", len(slice1))
		}

		// Add some items
		slice1 = append(slice1, "test1", "test2", "test3")
		pool.PutStringSlice(slice1)

		// Get slice again - should be cleared
		slice2 := pool.GetStringSlice()
		if len(slice2) != 0 {
			t.Errorf("Expected cleared slice, got length %d", len(slice2))
		}
	})
}

func TestSlicePoolConcurrency(t *testing.T) {
	const numGoroutines = 20
	const numOperations = 50

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				// Test Job slice pool
				slice := GetJobSlice()
				slice = append(slice, models.Job{ID: "test"})
				PutJobSlice(slice)

				// Test ScrapedJob slice pool
				scrapedSlice := GetScrapedJobSlice()
				scrapedSlice = append(scrapedSlice, models.ScrapedJob{Title: "test"})
				PutScrapedJobSlice(scrapedSlice)

				// Test string slice pool
				strSlice := GetStringSlice()
				strSlice = append(strSlice, "test")
				PutStringSlice(strSlice)
			}
		}()
	}

	wg.Wait()
	// If we get here without race conditions, the test passes
}

func BenchmarkObjectPool(b *testing.B) {
	b.Run("Job Pool", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			job := GetJob()
			job.ID = "test-id"
			job.Title = "test title"
			PutJob(job)
		}
	})

	b.Run("ScrapedJob Pool", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			job := GetScrapedJob()
			job.Title = "test title"
			job.URL = "http://example.com"
			PutScrapedJob(job)
		}
	})

	b.Run("Anchor Pool", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			anchor := GetAnchor()
			anchor.Text = "test text"
			anchor.Href = "http://example.com"
			PutAnchor(anchor)
		}
	})
}

func BenchmarkSlicePool(b *testing.B) {
	b.Run("Job Slice Pool", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			slice := GetJobSlice()
			slice = append(slice, models.Job{ID: "test"})
			PutJobSlice(slice)
		}
	})

	b.Run("ScrapedJob Slice Pool", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			slice := GetScrapedJobSlice()
			slice = append(slice, models.ScrapedJob{Title: "test"})
			PutScrapedJobSlice(slice)
		}
	})

	b.Run("String Slice Pool", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			slice := GetStringSlice()
			slice = append(slice, "test")
			PutStringSlice(slice)
		}
	})
}

func BenchmarkObjectPoolVsNew(b *testing.B) {
	b.Run("Object Pool", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			job := GetJob()
			job.ID = "test-id"
			job.Title = "test title"
			PutJob(job)
		}
	})

	b.Run("New Object", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			job := &models.Job{
				ID:    "test-id",
				Title: "test title",
			}
			_ = job
		}
	})
}
