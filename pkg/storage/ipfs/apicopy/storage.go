package apicopy

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"

	"github.com/filecoin-project/bacalhau/pkg/config"
	"github.com/filecoin-project/bacalhau/pkg/ipfs"
	"github.com/filecoin-project/bacalhau/pkg/storage"
	"github.com/filecoin-project/bacalhau/pkg/system"
	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel/trace"
)

// a storage driver runs the downloads content
// from a remote ipfs server and copies it to
// to a local directory in preparation for
// a job to run - it will remove the folder/file once complete

type StorageProvider struct {
	LocalDir   string
	IPFSClient *ipfs.Client
}

func NewStorageProvider(cm *system.CleanupManager, ipfsAPIAddress string) (*StorageProvider, error) {
	cl, err := ipfs.NewClient(ipfsAPIAddress)
	if err != nil {
		return nil, err
	}

	// TODO: consolidate the various config inputs into one package otherwise they are scattered across the codebase
	dir, err := ioutil.TempDir(config.GetStoragePath(), "bacalhau-ipfs")
	if err != nil {
		return nil, err
	}

	storageHandler := &StorageProvider{
		IPFSClient: cl,
		LocalDir:   dir,
	}

	log.Debug().Msgf("IPFS API Copy driver created with address: %s", ipfsAPIAddress)
	return storageHandler, nil
}

func (dockerIPFS *StorageProvider) IsInstalled(ctx context.Context) (bool, error) {
	ctx, span := newSpan(ctx, "IsInstalled")
	defer span.End()

	addresses, err := dockerIPFS.IPFSClient.SwarmAddresses(ctx)
	if err != nil {
		return false, err
	}

	return len(addresses) > 0, nil
}

func (dockerIPFS *StorageProvider) HasStorageLocally(ctx context.Context, volume storage.StorageSpec) (bool, error) {
	ctx, span := newSpan(ctx, "HasStorageLocally")
	defer span.End()
	return dockerIPFS.IPFSClient.HasCID(ctx, volume.Cid)
}

// we wrap this in a timeout because if the CID is not present on the network this seems to hang
func (dockerIPFS *StorageProvider) GetVolumeSize(ctx context.Context, volume storage.StorageSpec) (uint64, error) {
	ctx, span := newSpan(ctx, "GetVolumeResourceUsage")
	defer span.End()
	result, err := system.Timeout(config.GetVolumeSizeRequestTimeout(), func() (interface{}, error) {
		return dockerIPFS.IPFSClient.GetCidSize(ctx, volume.Cid)
	})
	if err != nil {
		if errors.Is(err, system.ErrorTimeout) {
			return 0, nil
		} else {
			return 0, err
		}
	}
	if uintResult, ok := result.(uint64); ok {
		return uintResult, nil
	} else {
		return 0, fmt.Errorf("error casting timeout result to uint64")
	}
}

func (dockerIPFS *StorageProvider) PrepareStorage(ctx context.Context, storageSpec storage.StorageSpec) (*storage.StorageVolume, error) {
	ctx, span := newSpan(ctx, "PrepareStorage")
	defer span.End()

	stat, err := dockerIPFS.IPFSClient.Stat(ctx, storageSpec.Cid)
	if err != nil {
		return nil, fmt.Errorf("failed to stat %s: %w", storageSpec.Cid, err)
	}

	if stat.Type != ipfs.IPLDFile && stat.Type != ipfs.IPLDDirectory {
		return nil, fmt.Errorf("unknown ipld file type for %s: %v", storageSpec.Cid, stat.Type)
	}

	var volume *storage.StorageVolume
	volume, err = dockerIPFS.copyFile(ctx, storageSpec)
	if err != nil {
		return nil, fmt.Errorf("failed to copy %s to volume: %w", storageSpec.Path, err)
	}

	return volume, nil
}

// nolint:lll // Exception to the long rule
func (dockerIPFS *StorageProvider) CleanupStorage(ctx context.Context, storageSpec storage.StorageSpec, volume *storage.StorageVolume) error {
	return system.RunCommand("sudo", []string{
		"rm", "-rf", fmt.Sprintf("%s/%s", dockerIPFS.LocalDir, storageSpec.Cid),
	})
}

func (dockerIPFS *StorageProvider) copyFile(ctx context.Context, storageSpec storage.StorageSpec) (*storage.StorageVolume, error) {
	outputPath := fmt.Sprintf("%s/%s", dockerIPFS.LocalDir, storageSpec.Cid)

	// If the output path already exists, we already have the data, as
	// ipfsClient.Get(...) renames the result path atomically after it has
	// finished downloading the CID.
	ok, err := system.PathExists(outputPath)
	if err != nil {
		return nil, err
	}
	if !ok {
		_, err := system.Timeout(config.GetDownloadCidRequestTimeout(), func() (interface{}, error) {
			innerErr := dockerIPFS.IPFSClient.Get(ctx, storageSpec.Cid, outputPath)
			return nil, innerErr
		})
		if err != nil {
			return nil, err
		}
	}

	volume := &storage.StorageVolume{
		Type:   "bind",
		Source: outputPath,
		Target: storageSpec.Path,
	}

	return volume, nil
}

func newSpan(ctx context.Context, apiName string) (context.Context, trace.Span) {
	return system.Span(ctx, "storage/ipfs/api_copy", apiName)
}

// Compile time interface check:
var _ storage.StorageProvider = (*StorageProvider)(nil)
