package common

import "github.com/QuantumNous/new-api/types"

// ResponsesRecoveryOutcome separates transport commitment from protocol success.
// A committed error is reported to channel health without replay or a JSON tail.
type ResponsesRecoveryOutcome struct {
	Error     *types.NewAPIError
	Committed bool
	// CommitEvent records the first SSE event that was actually released to the
	// client. It is intentionally event metadata only and must never contain the
	// response payload.
	CommitEvent string
	Penalize    bool
}
