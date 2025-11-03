package utils

import (
	"github.com/PuerkitoBio/goquery"
)

/*
	func MultipartWithFile(fields url.Values, filePath string) (io.Reader, string, error) {
		pr, pw := io.Pipe()
		w := multipart.NewWriter(pw)
		go func() {
			defer pw.Close()
			defer w.Close()

			// regular fields
			for k, vs := range fields {
				for _, v := range vs {
					_ = w.WriteField(k, v)
				}
			}

			// file part
			fn := filepath.Base(filePath)
			_ = mime.TypeByExtension(filepath.Ext(fn)) // best-effort mime registration
			fw, err := w.CreateFormFile("file", fn)
			if err != nil {
				_ = pw.CloseWithError(err)
				return
			}

			f, err := os.Open(filePath)
			if err != nil {
				_ = pw.CloseWithError(err)
				return
			}
			defer f.Close()

			if _, err := io.Copy(fw, f); err != nil {
				_ = pw.CloseWithError(err)
				return
			}
		}()
		return pr, w.FormDataContentType(), nil
	}
*/

func Attr(s *goquery.Selection, k, def string) string {
	if v, ok := s.Attr(k); ok {
		return v
	}
	return def
}
func Min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
func Max(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// String normalization helpers are provided by package strutil.
