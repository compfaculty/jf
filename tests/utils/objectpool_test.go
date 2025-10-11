package utils_test

import (
	"sync"
	"testing"

	"jf/internal/models"
	"jf/internal/utils"
)

func TestObjectPool(t *testing.T) {
	// Test Job pooling
	t.Run("Job Pool", func(t *testing.T) {
		job1 := utils.GetJob()
		job2 := utils.GetJob()

		if job1 == job2 {
			t.Error("Expected different job instances")
		}

		// Set some data
		job1.ID = "test-id"
		job1.Title = "test title"

		utils.PutJob(job1)

		// Get job again - should be reset
		job3 := utils.GetJob()
		if job3.ID != "" || job3.Title != "" {
			t.Error("Expected job to be reset after PutJob")
		}

		utils.PutJob(job2)
		utils.PutJob(job3)
	})

	// Test ScrapedJob pooling
	t.Run("ScrapedJob Pool", func(t *testing.T) {
		job1 := utils.GetScrapedJob()
		job2 := utils.GetScrapedJob()

		if job1 == job2 {
			t.Error("Expected different scraped job instances")
		}

		// Set some data
		job1.Title = "test title"
		job1.URL = "http://example.com"

		utils.PutScrapedJob(job1)

		// Get job again - should be reset
		job3 := utils.GetScrapedJob()
		if job3.Title != "" || job3.URL != "" {
			t.Error("Expected scraped job to be reset after PutScrapedJob")
		}

		utils.PutScrapedJob(job2)
		utils.PutScrapedJob(job3)
	})

	// Test Anchor pooling
	t.Run("Anchor Pool", func(t *testing.T) {
		anchor1 := utils.GetAnchor()
		anchor2 := utils.GetAnchor()

		if anchor1 == anchor2 {
			t.Error("Expected different anchor instances")
		}

		// Set some data
		anchor1.Text = "test text"
		anchor1.Href = "http://example.com"

		utils.PutAnchor(anchor1)

		// Get anchor again - should be reset
		anchor3 := utils.GetAnchor()
		if anchor3.Text != "" || anchor3.Href != "" {
			t.Error("Expected anchor to be reset after PutAnchor")
		}

		utils.PutAnchor(anchor2)
		utils.PutAnchor(anchor3)
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
				job := utils.GetJob()
				job.ID = "test-id"
				job.Title = "test title"
				utils.PutJob(job)

				// Test ScrapedJob pool
				scrapedJob := utils.GetScrapedJob()
				scrapedJob.Title = "scraped title"
				scrapedJob.URL = "http://example.com"
				utils.PutScrapedJob(scrapedJob)

				// Test Anchor pool
				anchor := utils.GetAnchor()
				anchor.Text = "anchor text"
				anchor.Href = "http://example.com"
				utils.PutAnchor(anchor)
			}
		}()
	}

	wg.Wait()
	// If we get here without race conditions, the test passes
}

func TestSlicePool(t *testing.T) {
	// Test Job slice pooling
	t.Run("Job Slice Pool", func(t *testing.T) {
		slice1 := utils.GetJobSlice()
		if cap(slice1) < 16 {
			t.Errorf("Expected capacity >= 16, got %d", cap(slice1))
		}
		if len(slice1) != 0 {
			t.Errorf("Expected length 0, got %d", len(slice1))
		}

		// Add some items
		slice1 = append(slice1, models.Job{ID: "1"}, models.Job{ID: "2"})
		utils.PutJobSlice(slice1)

		// Get slice again - should be cleared
		slice2 := utils.GetJobSlice()
		if len(slice2) != 0 {
			t.Errorf("Expected cleared slice, got length %d", len(slice2))
		}
	})

	// Test ScrapedJob slice pooling
	t.Run("ScrapedJob Slice Pool", func(t *testing.T) {
		slice1 := utils.GetScrapedJobSlice()
		if cap(slice1) < 16 {
			t.Errorf("Expected capacity >= 16, got %d", cap(slice1))
		}
		if len(slice1) != 0 {
			t.Errorf("Expected length 0, got %d", len(slice1))
		}

		// Add some items
		slice1 = append(slice1, models.ScrapedJob{Title: "test"}, models.ScrapedJob{Title: "test2"})
		utils.PutScrapedJobSlice(slice1)

		// Get slice again - should be cleared
		slice2 := utils.GetScrapedJobSlice()
		if len(slice2) != 0 {
			t.Errorf("Expected cleared slice, got length %d", len(slice2))
		}
	})

	// Test string slice pooling
	t.Run("String Slice Pool", func(t *testing.T) {
		slice1 := utils.GetStringSlice()
		if cap(slice1) < 16 {
			t.Errorf("Expected capacity >= 16, got %d", cap(slice1))
		}
		if len(slice1) != 0 {
			t.Errorf("Expected length 0, got %d", len(slice1))
		}

		// Add some items
		slice1 = append(slice1, "test1", "test2", "test3")
		utils.PutStringSlice(slice1)

		// Get slice again - should be cleared
		slice2 := utils.GetStringSlice()
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
				slice := utils.GetJobSlice()
				slice = append(slice, models.Job{ID: "test"})
				utils.PutJobSlice(slice)

				// Test ScrapedJob slice pool
				scrapedSlice := utils.GetScrapedJobSlice()
				scrapedSlice = append(scrapedSlice, models.ScrapedJob{Title: "test"})
				utils.PutScrapedJobSlice(scrapedSlice)

				// Test string slice pool
				strSlice := utils.GetStringSlice()
				strSlice = append(strSlice, "test")
				utils.PutStringSlice(strSlice)
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
			job := utils.GetJob()
			job.ID = "test-id"
			job.Title = "test title"
			utils.PutJob(job)
		}
	})

	b.Run("ScrapedJob Pool", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			job := utils.GetScrapedJob()
			job.Title = "test title"
			job.URL = "http://example.com"
			utils.PutScrapedJob(job)
		}
	})

	b.Run("Anchor Pool", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			anchor := utils.GetAnchor()
			anchor.Text = "test text"
			anchor.Href = "http://example.com"
			utils.PutAnchor(anchor)
		}
	})
}

func BenchmarkSlicePool(b *testing.B) {
	b.Run("Job Slice Pool", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			slice := utils.GetJobSlice()
			slice = append(slice, models.Job{ID: "test"})
			utils.PutJobSlice(slice)
		}
	})

	b.Run("ScrapedJob Slice Pool", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			slice := utils.GetScrapedJobSlice()
			slice = append(slice, models.ScrapedJob{Title: "test"})
			utils.PutScrapedJobSlice(slice)
		}
	})

	b.Run("String Slice Pool", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			slice := utils.GetStringSlice()
			slice = append(slice, "test")
			utils.PutStringSlice(slice)
		}
	})
}

func BenchmarkObjectPoolVsNew(b *testing.B) {
	b.Run("Object Pool", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			job := utils.GetJob()
			job.ID = "test-id"
			job.Title = "test title"
			utils.PutJob(job)
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
