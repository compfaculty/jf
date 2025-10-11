package utils

import (
	"sync"

	"jf/internal/models"
)

// ObjectPool provides object pooling for frequently created objects
type ObjectPool struct {
	jobPool        sync.Pool
	scrapedJobPool sync.Pool
	anchorPool     sync.Pool
}

// Global object pool instance
var globalObjectPool = &ObjectPool{
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

// Anchor represents a link extracted from a page
type Anchor struct {
	Text string
	Href string
}

// GetJob returns a Job from the pool
func (p *ObjectPool) GetJob() *models.Job {
	return p.jobPool.Get().(*models.Job)
}

// PutJob returns a Job to the pool after resetting it
func (p *ObjectPool) PutJob(job *models.Job) {
	if job == nil {
		return
	}
	// Reset the job fields
	*job = models.Job{}
	p.jobPool.Put(job)
}

// GetScrapedJob returns a ScrapedJob from the pool
func (p *ObjectPool) GetScrapedJob() *models.ScrapedJob {
	return p.scrapedJobPool.Get().(*models.ScrapedJob)
}

// PutScrapedJob returns a ScrapedJob to the pool after resetting it
func (p *ObjectPool) PutScrapedJob(job *models.ScrapedJob) {
	if job == nil {
		return
	}
	// Reset the scraped job fields
	*job = models.ScrapedJob{}
	p.scrapedJobPool.Put(job)
}

// GetAnchor returns an Anchor from the pool
func (p *ObjectPool) GetAnchor() *Anchor {
	return p.anchorPool.Get().(*Anchor)
}

// PutAnchor returns an Anchor to the pool after resetting it
func (p *ObjectPool) PutAnchor(anchor *Anchor) {
	if anchor == nil {
		return
	}
	// Reset the anchor fields
	*anchor = Anchor{}
	p.anchorPool.Put(anchor)
}

// Convenience functions using the global pool
func GetJob() *models.Job {
	return globalObjectPool.GetJob()
}

func PutJob(job *models.Job) {
	globalObjectPool.PutJob(job)
}

func GetScrapedJob() *models.ScrapedJob {
	return globalObjectPool.GetScrapedJob()
}

func PutScrapedJob(job *models.ScrapedJob) {
	globalObjectPool.PutScrapedJob(job)
}

func GetAnchor() *Anchor {
	return globalObjectPool.GetAnchor()
}

func PutAnchor(anchor *Anchor) {
	globalObjectPool.PutAnchor(anchor)
}

// SlicePool provides pooling for slices of common types
type SlicePool struct {
	jobSlicePool        sync.Pool
	scrapedJobSlicePool sync.Pool
	stringSlicePool     sync.Pool
}

// Global slice pool instance
var globalSlicePool = &SlicePool{
	jobSlicePool: sync.Pool{
		New: func() interface{} {
			return make([]models.Job, 0, 16) // Pre-allocate with capacity
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

// GetJobSlice returns a Job slice from the pool
func (p *SlicePool) GetJobSlice() []models.Job {
	return p.jobSlicePool.Get().([]models.Job)
}

// PutJobSlice returns a Job slice to the pool after clearing it
func (p *SlicePool) PutJobSlice(slice []models.Job) {
	if slice == nil {
		return
	}
	// Clear the slice but keep the underlying array
	slice = slice[:0]
	p.jobSlicePool.Put(slice)
}

// GetScrapedJobSlice returns a ScrapedJob slice from the pool
func (p *SlicePool) GetScrapedJobSlice() []models.ScrapedJob {
	return p.scrapedJobSlicePool.Get().([]models.ScrapedJob)
}

// PutScrapedJobSlice returns a ScrapedJob slice to the pool after clearing it
func (p *SlicePool) PutScrapedJobSlice(slice []models.ScrapedJob) {
	if slice == nil {
		return
	}
	slice = slice[:0]
	p.scrapedJobSlicePool.Put(slice)
}

// GetStringSlice returns a string slice from the pool
func (p *SlicePool) GetStringSlice() []string {
	return p.stringSlicePool.Get().([]string)
}

// PutStringSlice returns a string slice to the pool after clearing it
func (p *SlicePool) PutStringSlice(slice []string) {
	if slice == nil {
		return
	}
	slice = slice[:0]
	p.stringSlicePool.Put(slice)
}

// Convenience functions using the global slice pool
func GetJobSlice() []models.Job {
	return globalSlicePool.GetJobSlice()
}

func PutJobSlice(slice []models.Job) {
	globalSlicePool.PutJobSlice(slice)
}

func GetScrapedJobSlice() []models.ScrapedJob {
	return globalSlicePool.GetScrapedJobSlice()
}

func PutScrapedJobSlice(slice []models.ScrapedJob) {
	globalSlicePool.PutScrapedJobSlice(slice)
}

func GetStringSlice() []string {
	return globalSlicePool.GetStringSlice()
}

func PutStringSlice(slice []string) {
	globalSlicePool.PutStringSlice(slice)
}
