package capacitymanager

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"syscall"

	"github.com/BTBurke/k8sresource"
	"github.com/c2h5oh/datasize"
	"github.com/filecoin-project/bacalhau/pkg/config"
	"github.com/pbnjay/memory"
)

func newDefaultResourceUsageConfig() ResourceUsageConfig {
	return ResourceUsageConfig{
		CPU:    "",
		Memory: "",
		Disk:   "",
	}
}

func newResourceUsageConfig(cpu, mem, disk string) ResourceUsageConfig {
	return ResourceUsageConfig{
		CPU:    cpu,
		Memory: mem,
		Disk:   disk,
	}
}

// allow Mi, Gi to mean Mb, Gb
// remove spaces
// lowercase
func convertBytesString(st string) string {
	st = strings.ToLower(st)
	st = strings.ReplaceAll(st, "i", "b")
	st = strings.ReplaceAll(st, " ", "")
	return st
}

func convertCPUStringWithError(val string) (float64, error) {
	if val == "" {
		return 0, nil
	}
	cpu, err := k8sresource.NewCPUFromString(convertBytesString(val))
	if err != nil {
		return 0, err
	}
	return cpu.ToFloat64(), nil
}

func ConvertCPUString(val string) float64 {
	ret, err := convertCPUStringWithError(val)
	if err != nil {
		return 0
	}
	return ret
}

func convertMemoryStringWithError(val string) (uint64, error) {
	if val == "" {
		return 0, nil
	}
	mem, err := datasize.ParseString(convertBytesString(val))
	if err != nil {
		return 0, err
	}
	return mem.Bytes(), nil
}

func ConvertMemoryString(val string) uint64 {
	ret, err := convertMemoryStringWithError(val)
	if err != nil {
		return 0
	}
	return ret
}

func ParseResourceUsageConfig(usage ResourceUsageConfig) ResourceUsageData {
	return ResourceUsageData{
		CPU:    ConvertCPUString(usage.CPU),
		Memory: ConvertMemoryString(usage.Memory),
		Disk:   ConvertMemoryString(usage.Disk),
	}
}

func getResourceUsageConfig(usage ResourceUsageData) (ResourceUsageConfig, error) {
	c := ResourceUsageConfig{}

	cpu := k8sresource.NewCPUFromFloat(usage.CPU)

	c.CPU = cpu.ToString()
	c.Memory = (datasize.ByteSize(usage.Memory) * datasize.B).String()
	c.Disk = (datasize.ByteSize(usage.Disk) * datasize.B).String()

	return c, nil
}

// get free disk space for storage path
// returns bytes
func getFreeDiskSpace(path string) (uint64, error) {
	fs := syscall.Statfs_t{}
	err := syscall.Statfs(path, &fs)
	if err != nil {
		return 0, err
	}
	return fs.Bfree * uint64(fs.Bsize), nil
}

// what resources does this compute node actually have?
func getSystemResources(limitConfig ResourceUsageConfig) (ResourceUsageData, error) {

	// this is used mainly for tests to be deterministic
	allowOverCommit := os.Getenv("BACALHAU_CAPACITY_MANAGER_OVER_COMMIT") != ""

	diskSpace, err := getFreeDiskSpace(config.GetStoragePath())
	if err != nil {
		return ResourceUsageData{}, err
	}

	// the actual resources we have
	physcialResources := ResourceUsageData{
		CPU:    float64(runtime.NumCPU()),
		Memory: memory.TotalMemory(),
		Disk:   diskSpace,
	}

	parsedLimitConfig := ParseResourceUsageConfig(limitConfig)

	if parsedLimitConfig.CPU > 0 {
		if parsedLimitConfig.CPU > physcialResources.CPU && !allowOverCommit {
			return physcialResources, fmt.Errorf(
				"you cannot configure more CPU than you have on this node: configured %f, have %f",
				parsedLimitConfig.CPU, physcialResources.CPU,
			)
		}
		physcialResources.CPU = parsedLimitConfig.CPU
	}

	if parsedLimitConfig.Memory > 0 {
		if parsedLimitConfig.Memory > physcialResources.Memory && !allowOverCommit {
			return physcialResources, fmt.Errorf(
				"you cannot configure more Memory than you have on this node: configured %d, have %d",
				parsedLimitConfig.Memory, physcialResources.Memory,
			)
		}
		physcialResources.Memory = parsedLimitConfig.Memory
	}

	if parsedLimitConfig.Disk > 0 {
		if parsedLimitConfig.Disk > physcialResources.Disk && !allowOverCommit {
			return physcialResources, fmt.Errorf(
				"you cannot configure more disk than you have on this node: configured %d, have %d",
				parsedLimitConfig.Disk, physcialResources.Disk,
			)
		}
		physcialResources.Disk = parsedLimitConfig.Disk
	}

	return physcialResources, nil
}

// given a "required" usage and a "limit" of usage - can we run the requirement
func checkResourceUsage(wants, limits ResourceUsageData) bool {
	// if there are some limits and there are zero values for "wants"
	// we deny the job because we can't know if it would exceed our limit
	if wants.CPU <= 0 && wants.Memory <= 0 && wants.Disk <= 0 && (limits.CPU > 0 || limits.Memory > 0 || limits.Disk > 0) {
		return false
	}
	return wants.CPU <= limits.CPU && wants.Memory <= limits.Memory && wants.Disk <= limits.Disk
}

func subtractResourceUsage(current, totals ResourceUsageData) ResourceUsageData {
	return ResourceUsageData{
		CPU:    totals.CPU - current.CPU,
		Memory: totals.Memory - current.Memory,
		Disk:   totals.Disk - current.Disk,
	}
}