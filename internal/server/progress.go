package server

type ScanStatus struct {
	Running   bool   `json:"running"`
	StartedAt string `json:"started_at"`
	Percent   int    `json:"percent"`
	Found     int    `json:"found"`
	Total     int    `json:"total"`
	Error     string `json:"error"`
}
