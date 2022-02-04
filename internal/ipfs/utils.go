package ipfs

import (
	"fmt"
	"strings"

	"github.com/filecoin-project/bacalhau/internal/system"
)

const IPFS_REPO_LOCATION string = "data/ipfs"

func GetIpfsRepo(hostId string) string {
	return fmt.Sprintf("%s/%s", IPFS_REPO_LOCATION, hostId)
}

func EnsureIpfsRepo(hostId string) (string, error) {
	folder := GetIpfsRepo(hostId)
	err := system.RunCommand("mkdir", []string{
		"-p",
		folder,
	})
	return folder, err
}

func IpfsCommand(repoPath string, args []string) (string, error) {
	return system.RunCommandGetResultsEnv("ipfs", args, []string{
		"IPFS_PATH=" + repoPath,
	})
}

func InitializeRepo(repoPath string) error {
	_, err := IpfsCommand(repoPath, []string{
		"init",
	})
	return err
}

func HasCid(repoPath, cid string) (bool, error) {
	allLocalRefString, err := IpfsCommand(repoPath, []string{
		"refs",
		"local",
	})
	if err != nil {
		return false, err
	}
	return contains(strings.Split(allLocalRefString, "\n"), cid), nil
}

func contains(s []string, e string) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}
