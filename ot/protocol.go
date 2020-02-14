package ot

import (
	"github.com/fmpwizard/go-quilljs-delta/delta"
)

// This struct serves 2 purposes:
//
// * When Delta is present, it is a change message
// * When Delta is missing, it serves as an ack message
type Change struct {
	Delta   *delta.Delta
	Version uint32
}
