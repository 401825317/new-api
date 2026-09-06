package common

import "github.com/QuantumNous/new-api/types"

// ResponsesRecoveryOutcome separates transport commitment from protocol success.
// A committed error is reported to channel health without replay or a JSON tail.
type ResponsesRecoveryOutcome struct {
	Error     *types.NewAPIError
	Committed bool
	Penalize  bool
}
