package util

import (
	"jf/internal/models"
	_ "jf/internal/models"
	"jf/internal/validators"
)

func DedupeByURL(in []models.Job) []models.Job {
	seen := make(map[string]struct{}, len(in))
	out := make([]models.Job, 0, len(in))
	for _, j := range in {
		key := validators.CanonURL(j.URL)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, j)
	}
	return out
}
