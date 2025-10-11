package utils

import (
	"encoding/json"
	"jf/internal/models"
	"jf/internal/strutil"
	"os"
	"sync"

	"sort"
	"strings"
)

// Tiny alias so we can switch easily if needed
type WG = sync.WaitGroup

func DedupeJobs(in []models.Job) []models.Job {
	seenTU := map[[2]string]struct{}{}
	seenFB := map[string]struct{}{}
	out := make([]models.Job, 0, len(in))
	for _, j := range in {
		t := strutil.Normalize(j.Title)
		u := strutil.Normalize(j.URL)
		if t != "" || u != "" {
			k := [2]string{t, u}
			if _, ok := seenTU[k]; ok {
				continue
			}
			seenTU[k] = struct{}{}
			out = append(out, j)
			continue
		}
		fb := strutil.SHA16(strings.TrimSpace(j.Title) + " | " + strings.TrimSpace(j.URL))
		if _, ok := seenFB[fb]; ok {
			continue
		}
		seenFB[fb] = struct{}{}
		out = append(out, j)
	}
	return out
}

type pair[T any] struct {
	idx int
	v   T
}

func SortPairs[T any](a []pair[T]) {
	sort.SliceStable(a, func(i, j int) bool { return a[i].idx < a[j].idx })
}

func WriteJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func ReadCompaniesFromJson(path string) ([]models.Company, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var arr []models.Company
	if err := json.Unmarshal(b, &arr); err != nil {
		return nil, err
	}
	return arr, nil
}
