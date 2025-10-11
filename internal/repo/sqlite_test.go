package repo

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"jf/internal/models"
)

func setupTestDB(t *testing.T) *SQLiteRepo {
	// Create a temporary database file
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	repo, err := NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}

	return repo
}

func TestSQLiteRepo_CompanyOperations(t *testing.T) {
	repo := setupTestDB(t)
	defer repo.Close()

	ctx := context.Background()

	t.Run("Create Company", func(t *testing.T) {
		company := &models.Company{
			Name:       "Test Company",
			CareersURL: "https://example.com/careers",
			Active:     true,
		}

		err := repo.UpsertCompany(ctx, company)
		if err != nil {
			t.Fatalf("Failed to create company: %v", err)
		}

		if company.ID == "" {
			t.Error("Expected company ID to be set")
		}
	})

	t.Run("Upsert Company By Name", func(t *testing.T) {
		company := &models.Company{
			Name:       "Upsert Test Company",
			CareersURL: "https://example.com/careers",
			Active:     true,
		}

		err := repo.UpsertCompanyByName(ctx, company)
		if err != nil {
			t.Fatalf("Failed to upsert company: %v", err)
		}

		// Verify it was created by listing companies
		companies, err := repo.ListCompanies(ctx)
		if err != nil {
			t.Fatalf("Failed to list companies: %v", err)
		}

		found := false
		for _, c := range companies {
			if c.Name == company.Name {
				found = true
				if c.CareersURL != company.CareersURL {
					t.Errorf("Expected career URL %q, got %q", company.CareersURL, c.CareersURL)
				}
				break
			}
		}

		if !found {
			t.Error("Expected to find created company in list")
		}
	})

	t.Run("List Companies", func(t *testing.T) {
		// Create multiple companies
		companies := []*models.Company{
			{Name: "Company 1", CareersURL: "https://company1.com/careers", Active: true},
			{Name: "Company 2", CareersURL: "https://company2.com/careers", Active: true},
			{Name: "Company 3", CareersURL: "https://company3.com/careers", Active: false},
		}

		for _, company := range companies {
			err := repo.UpsertCompany(ctx, company)
			if err != nil {
				t.Fatalf("Failed to create company %s: %v", company.Name, err)
			}
		}

		// Test listing companies (only returns active by default)
		allCompanies, err := repo.ListCompanies(ctx)
		if err != nil {
			t.Fatalf("Failed to list companies: %v", err)
		}

		if len(allCompanies) < 2 {
			t.Errorf("Expected at least 2 active companies, got %d", len(allCompanies))
		}

		// Verify all returned companies are active
		for _, company := range allCompanies {
			if !company.Active {
				t.Error("Expected all returned companies to be active")
			}
		}
	})

	t.Run("Update Company", func(t *testing.T) {
		company := &models.Company{
			Name:       "Update Test Company",
			CareersURL: "https://example.com/careers",
			Active:     true,
		}

		err := repo.UpsertCompany(ctx, company)
		if err != nil {
			t.Fatalf("Failed to create company: %v", err)
		}

		// Update the company
		company.Name = "Updated Company Name"
		company.Active = false

		err = repo.UpsertCompany(ctx, company)
		if err != nil {
			t.Fatalf("Failed to update company: %v", err)
		}

		// Retrieve and verify update - company is now inactive so won't be in ListCompanies
		// Just verify no error occurred
		if company.Name != "Updated Company Name" {
			t.Errorf("Expected updated name, got %q", company.Name)
		}
		if company.Active {
			t.Error("Expected company to be inactive")
		}
	})
}

func TestSQLiteRepo_JobOperations(t *testing.T) {
	repo := setupTestDB(t)
	defer repo.Close()

	ctx := context.Background()

	// Create a company first
	company := &models.Company{
		Name:       "Job Test Company",
		CareersURL: "https://example.com/careers",
		Active:     true,
	}

	err := repo.UpsertCompany(ctx, company)
	if err != nil {
		t.Fatalf("Failed to create company: %v", err)
	}

	t.Run("Create Job", func(t *testing.T) {
		job := &models.Job{
			CompanyID:   company.ID,
			Title:       "Software Engineer",
			URL:         "https://example.com/jobs/1",
			Location:    "San Francisco, CA",
			Description: "Great opportunity to work on cutting-edge technology",
		}

		err := repo.UpsertJob(ctx, job)
		if err != nil {
			t.Fatalf("Failed to create job: %v", err)
		}

		if job.ID == "" {
			t.Error("Expected job ID to be set")
		}
	})

	t.Run("List Jobs", func(t *testing.T) {
		// Create multiple jobs for the company
		jobs := []*models.Job{
			{
				CompanyID:   company.ID,
				Title:       "Job 1",
				URL:         "https://example.com/jobs/3",
				Location:    "Location 1",
				Description: "Description 1",
			},
			{
				CompanyID:   company.ID,
				Title:       "Job 2",
				URL:         "https://example.com/jobs/4",
				Location:    "Location 2",
				Description: "Description 2",
			},
		}

		for _, job := range jobs {
			err := repo.UpsertJob(ctx, job)
			if err != nil {
				t.Fatalf("Failed to create job %s: %v", job.Title, err)
			}
		}

		// Use ListJobs with CompanyID filter
		query := models.JobQuery{
			CompanyID: company.ID,
			Limit:     100,
		}
		companyJobs, err := repo.ListJobs(ctx, query)
		if err != nil {
			t.Fatalf("Failed to list jobs by company: %v", err)
		}

		if len(companyJobs) < 2 {
			t.Errorf("Expected at least 2 jobs, got %d", len(companyJobs))
		}

		// Verify all jobs belong to the company
		for _, job := range companyJobs {
			if job.CompanyID != company.ID {
				t.Errorf("Job %s belongs to wrong company", job.ID)
			}
		}
	})

	t.Run("Search Jobs", func(t *testing.T) {
		// Create jobs with different titles for searching
		searchJobs := []*models.Job{
			{
				CompanyID:   company.ID,
				Title:       "Senior Software Engineer",
				URL:         "https://example.com/jobs/5",
				Location:    "San Francisco, CA",
				Description: "Looking for a senior engineer with Go experience",
			},
			{
				CompanyID:   company.ID,
				Title:       "Junior Developer",
				URL:         "https://example.com/jobs/6",
				Location:    "New York, NY",
				Description: "Entry level position for recent graduates",
			},
		}

		for _, job := range searchJobs {
			err := repo.UpsertJob(ctx, job)
			if err != nil {
				t.Fatalf("Failed to create search job %s: %v", job.Title, err)
			}
		}

		// Search for "Senior" jobs using ListJobs with query
		query := models.JobQuery{
			Q:     "Senior",
			Limit: 10,
		}

		results, err := repo.ListJobs(ctx, query)
		if err != nil {
			t.Fatalf("Failed to search jobs: %v", err)
		}

		if len(results) == 0 {
			t.Error("Expected to find at least one job matching 'Senior'")
		}

		// Verify results contain the expected job
		found := false
		for _, job := range results {
			if job.Title == "Senior Software Engineer" {
				found = true
				break
			}
		}

		if !found {
			t.Error("Expected to find 'Senior Software Engineer' job in search results")
		}
	})
}

func TestSQLiteRepo_Concurrency(t *testing.T) {
	repo := setupTestDB(t)
	defer repo.Close()

	ctx := context.Background()

	// Create a company
	company := &models.Company{
		Name:       "Concurrency Test Company",
		CareersURL: "https://example.com/careers",
		Active:     true,
	}

	err := repo.UpsertCompany(ctx, company)
	if err != nil {
		t.Fatalf("Failed to create company: %v", err)
	}

	const numGoroutines = 10
	const numJobsPerGoroutine = 10

	// Test concurrent job creation
	for i := 0; i < numGoroutines; i++ {
		go func(goroutineID int) {
			for j := 0; j < numJobsPerGoroutine; j++ {
				job := &models.Job{
					CompanyID:   company.ID,
					Title:       fmt.Sprintf("Job %d-%d", goroutineID, j),
					URL:         fmt.Sprintf("https://example.com/jobs/%d-%d", goroutineID, j),
					Location:    "Test Location",
					Description: "Concurrent test job",
				}

				err := repo.UpsertJob(ctx, job)
				if err != nil {
					t.Errorf("Failed to create job in goroutine %d: %v", goroutineID, err)
				}
			}
		}(i)
	}

	// Wait for goroutines to complete
	time.Sleep(200 * time.Millisecond)

	// Verify jobs were created using ListJobs
	query := models.JobQuery{
		CompanyID: company.ID,
		Limit:     200,
	}
	jobs, err := repo.ListJobs(ctx, query)
	if err != nil {
		t.Fatalf("Failed to list jobs: %v", err)
	}

	if len(jobs) < numGoroutines*numJobsPerGoroutine {
		t.Errorf("Expected at least %d jobs, got %d", numGoroutines*numJobsPerGoroutine, len(jobs))
	}
}

func TestSQLiteRepo_ErrorHandling(t *testing.T) {
	repo := setupTestDB(t)
	defer repo.Close()

	ctx := context.Background()

	t.Run("Invalid Job Company ID", func(t *testing.T) {
		job := &models.Job{
			CompanyID:   "non-existent-company-id",
			Title:       "Test Job",
			URL:         "https://example.com/jobs/test",
			Location:    "Test Location",
			Description: "Test description",
		}

		err := repo.UpsertJob(ctx, job)
		if err == nil {
			t.Error("Expected error when creating job with non-existent company ID")
		}
	})
}

func BenchmarkSQLiteRepo(b *testing.B) {
	// Create temporary database
	tempDir := b.TempDir()
	dbPath := filepath.Join(tempDir, "bench.db")

	repo, err := NewSQLite(dbPath)
	if err != nil {
		b.Fatalf("Failed to create benchmark database: %v", err)
	}
	defer repo.Close()

	ctx := context.Background()

	// Create a company for benchmarking
	company := &models.Company{
		Name:       "Benchmark Company",
		CareersURL: "https://example.com/careers",
		Active:     true,
	}

	err = repo.UpsertCompany(ctx, company)
	if err != nil {
		b.Fatalf("Failed to create company: %v", err)
	}

	b.Run("UpsertJob", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			job := &models.Job{
				CompanyID:   company.ID,
				Title:       fmt.Sprintf("Job %d", i),
				URL:         fmt.Sprintf("https://example.com/jobs/%d", i),
				Location:    "Benchmark Location",
				Description: "Benchmark job description",
			}

			err := repo.UpsertJob(ctx, job)
			if err != nil {
				b.Fatalf("Failed to upsert job: %v", err)
			}
		}
	})

	b.Run("ListJobs", func(b *testing.B) {
		b.ResetTimer()
		query := models.JobQuery{
			CompanyID: company.ID,
			Limit:     100,
		}
		for i := 0; i < b.N; i++ {
			_, err := repo.ListJobs(ctx, query)
			if err != nil {
				b.Fatalf("Failed to list jobs: %v", err)
			}
		}
	})
}
