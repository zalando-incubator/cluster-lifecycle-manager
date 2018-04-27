package api

// Problem describes a problem.
type Problem struct {
	Detail   string `json:"detail"   yaml:"detail"`
	Instance string `json:"instance" yaml:"instance"`
	Status   int32  `json:"status"   yaml:"status"`
	Title    string `json:"title"    yaml:"title"`
	Type     string `json:"type"     yaml:"type"`
}
