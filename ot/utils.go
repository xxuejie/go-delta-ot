package ot

import (
	"fmt"

	"github.com/fmpwizard/go-quilljs-delta/delta"
)

func cloneDelta(d *delta.Delta) *delta.Delta {
	ops := make([]delta.Op, len(d.Ops))
	for i, op := range d.Ops {
		ops[i] = op
	}
	return delta.New(ops)
}

type File struct {
	d       delta.Delta
	version uint32
	// Reverts serve 2 purposes:
	//
	// * Provide revert function
	// * Keep old versions of the document for slow clients
	reverts []delta.Delta
}

func NewFile(d delta.Delta) *File {
	return &File{
		d:       d,
		version: 0,
		reverts: make([]delta.Delta, 0),
	}
}

func (f *File) CurrentChange() Change {
	return Change{
		Version: f.version,
		Delta:   cloneDelta(&f.d),
	}
}

func (f *File) Submit(change Change) (Change, error) {
	if change.Delta == nil {
		return Change{}, fmt.Errorf("Submitted change must have delta!")
	}
	if change.Version > f.version {
		return Change{}, fmt.Errorf("Invalid change version %d, current version: %d", change.Version, f.version)
	} else if change.Version < f.version {
		revertedVersions := int(f.version - change.Version)
		if revertedVersions <= 0 || revertedVersions > len(f.reverts) {
			return Change{}, fmt.Errorf("Submitted version %d is too old, oldest version now: %d", change.Version, int(f.version)-len(f.reverts))
		}

		revertedOperation := delta.New(nil)
		for i := 0; i < revertedVersions; i++ {
			revertedOperation = revertedOperation.Compose(f.reverts[len(f.reverts)-1-i])
		}
		operation := *revertedOperation.Invert(&f.d)

		change.Delta = operation.Transform(*change.Delta, true)
		change.Version = f.version
	}
	revert := *change.Delta.Invert(&f.d)
	f.d = *f.d.Compose(*change.Delta)
	f.version += 1
	f.reverts = append(f.reverts, revert)

	return Change{
		Delta:   cloneDelta(change.Delta),
		Version: f.version,
	}, nil
}
