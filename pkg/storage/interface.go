package storage

import (
	"github.com/filecoin-project/bacalhau/pkg/types"
)

type PreparedStorageVolume struct {
	Type   string
	Source string
	Target string
}

type StorageProvider interface {
	IsInstalled() (bool, error)
	HasStorage(volume types.StorageSpec) (bool, error)
	PrepareStorage(volume types.StorageSpec) (*PreparedStorageVolume, error)
}