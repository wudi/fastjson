//go:build amd64

package fastjson

import "testing"

// TestPureGoFallback exercises the non-amd64 code paths by forcing the
// hasFastScan runtime flag off and running the full correctness suite's
// hottest tests. This catches the class of bug where a new call site
// forgets to gate on hasFastScan and would break arm64 / older amd64.
func TestPureGoFallback(t *testing.T) {
	saved := hasFastScan
	hasFastScan = false
	defer func() { hasFastScan = saved }()

	// Re-run the core correctness checks against the pure-Go paths.
	// Each of these would catch a hasFastScan-gating regression in its
	// respective kernel:
	t.Run("schubfach_seed", TestSchubfachSeedCorpus)
	t.Run("schubfach_canada", TestSchubfachCanadaRoundtrip)
	t.Run("writedigits_parity", TestWriteDigitsAsmParity)
}
