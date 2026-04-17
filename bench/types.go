package bench

// Minimal structural types for corpus decoding. We use interface{} for many
// corpus files to fairly exercise the generic decode path — that is the hot
// path most apps hit. Library benchmarks (sonic, goccy) all use the same
// interface{} / map[string]interface{} target for corpus payloads.

type SmallUser struct {
	ID        int64    `json:"id"`
	Name      string   `json:"name"`
	Email     string   `json:"email"`
	Age       int      `json:"age"`
	Active    bool     `json:"active"`
	Score     float64  `json:"score"`
	Tags      []string `json:"tags"`
	CreatedAt string   `json:"created_at"`
}
