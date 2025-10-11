package utils

import (
	"jf/internal/models"
	"jf/internal/strutil"
)

func DedupeByURL(in []models.Job) []models.Job {
	seen := make(map[string]struct{}, len(in))
	out := make([]models.Job, 0, len(in))
	for _, j := range in {
		key := strutil.CanonURL(j.URL)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, j)
	}
	return out
}
