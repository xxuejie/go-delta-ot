package ot

import (
	"github.com/fmpwizard/go-quilljs-delta/delta"
)

// This struct serves 2 purposes:
//
// * When Delta is present, it is a change message
// * When Delta is missing, it serves as an ack message
type Change struct {
	Delta   *delta.Delta `json:"delta,omitempty"`
	Version uint32       `json:"version"`
}

type MultiFileChange struct {
	Id     uint32 `json:"id"`
	Change Change `json:"change"`
}
