//go:build amd64

package fastjson

import "testing"

// TestPureGoFallback exercises the non-amd64 code paths by forcing the
// hasAVX512 runtime flag off and running the full correctness suite's
// hottest tests. This catches the class of bug where a new call site
// forgets to gate on hasAVX512 and would break arm64 / older amd64.
func TestPureGoFallback(t *testing.T) {
	saved := hasAVX512
	hasAVX512 = false
	defer func() { hasAVX512 = saved }()

	// Re-run the core correctness checks against the pure-Go paths.
	// Each of these would catch a hasAVX512-gating regression in its
	// respective kernel:
	t.Run("schubfach_seed", TestSchubfachSeedCorpus)
	t.Run("schubfach_canada", TestSchubfachCanadaRoundtrip)
	t.Run("writedigits_parity", TestWriteDigitsAsmParity)
}
