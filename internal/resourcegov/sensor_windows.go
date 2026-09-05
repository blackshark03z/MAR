//go:build windows

package resourcegov

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32Resource         = windows.NewLazySystemDLL("kernel32.dll")
	user32Resource           = windows.NewLazySystemDLL("user32.dll")
	procGetSystemTimes       = kernel32Resource.NewProc("GetSystemTimes")
	procGlobalMemoryStatusEx = kernel32Resource.NewProc("GlobalMemoryStatusEx")
	procGetTickCount         = kernel32Resource.NewProc("GetTickCount")
	procGetLastInputInfo     = user32Resource.NewProc("GetLastInputInfo")
)

type WindowsSensorConfig struct {
	DiskPath                 string
	MARRoots                 []string
	InteractiveIdleThreshold time.Duration
	DiskUsageCacheTTL        time.Duration
}

type WindowsSensor struct {
	cfg WindowsSensorConfig

	mu         sync.Mutex
	haveCPU    bool
	prevIdle   uint64
	prevKernel uint64
	prevUser   uint64

	diskCachedAt time.Time
	diskCached   uint64
}

func NewWindowsSensor(cfg WindowsSensorConfig) (*WindowsSensor, error) {
	if cfg.DiskPath == "" {
		return nil, fmt.Errorf("disk path is required")
	}
	if cfg.InteractiveIdleThreshold <= 0 {
		return nil, fmt.Errorf("interactive idle threshold must be positive")
	}
	if cfg.DiskUsageCacheTTL < 0 {
		return nil, fmt.Errorf("disk usage cache TTL cannot be negative")
	}
	return &WindowsSensor{cfg: cfg}, nil
}

type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

type lastInputInfo struct {
	Size uint32
	Time uint32
}

func (s *WindowsSensor) Snapshot(ctx context.Context) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}

	cpu, cpuKnown, err := s.cpuPercent()
	if err != nil {
		return Snapshot{}, err
	}
	mem, err := memorySnapshot()
	if err != nil {
		return Snapshot{}, err
	}
	freeDisk, totalDisk, err := diskSnapshot(s.cfg.DiskPath)
	if err != nil {
		return Snapshot{}, err
	}
	interactive, err := userInteractive(s.cfg.InteractiveIdleThreshold)
	if err != nil {
		return Snapshot{}, err
	}
	marDisk, err := s.marDiskUsage(ctx)
	if err != nil {
		return Snapshot{}, err
	}

	return Snapshot{
		ObservedAt:        time.Now().UTC(),
		CPUPercent:        cpu,
		CPUKnown:          cpuKnown,
		MemoryLoadPercent: float64(mem.MemoryLoad),
		TotalRAMBytes:     mem.TotalPhys,
		AvailableRAMBytes: mem.AvailPhys,
		FreeDiskBytes:     freeDisk,
		TotalDiskBytes:    totalDisk,
		MARDiskUsedBytes:  marDisk,
		UserInteractive:   interactive,
		IOPressureKnown:   false,
	}, nil
}

func (s *WindowsSensor) cpuPercent() (float64, bool, error) {
	var idle, kernel, user windows.Filetime
	r1, _, callErr := procGetSystemTimes.Call(
		uintptr(unsafe.Pointer(&idle)),
		uintptr(unsafe.Pointer(&kernel)),
		uintptr(unsafe.Pointer(&user)),
	)
	if r1 == 0 {
		return 0, false, fmt.Errorf("GetSystemTimes: %w", callErr)
	}
	idleTicks := filetimeTicks(idle)
	kernelTicks := filetimeTicks(kernel)
	userTicks := filetimeTicks(user)

	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.haveCPU {
		s.haveCPU = true
		s.prevIdle, s.prevKernel, s.prevUser = idleTicks, kernelTicks, userTicks
		return 0, false, nil
	}
	idleDelta := idleTicks - s.prevIdle
	kernelDelta := kernelTicks - s.prevKernel
	userDelta := userTicks - s.prevUser
	s.prevIdle, s.prevKernel, s.prevUser = idleTicks, kernelTicks, userTicks
	total := kernelDelta + userDelta
	if total == 0 {
		return 0, false, nil
	}
	busy := total
	if idleDelta < total {
		busy -= idleDelta
	} else {
		busy = 0
	}
	percent := 100 * float64(busy) / float64(total)
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	return percent, true, nil
}

func memorySnapshot() (memoryStatusEx, error) {
	var mem memoryStatusEx
	mem.Length = uint32(unsafe.Sizeof(mem))
	r1, _, callErr := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&mem)))
	if r1 == 0 {
		return memoryStatusEx{}, fmt.Errorf("GlobalMemoryStatusEx: %w", callErr)
	}
	return mem, nil
}

func diskSnapshot(path string) (free, total uint64, err error) {
	ptr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, err
	}
	var callerFree, totalBytes, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(ptr, &callerFree, &totalBytes, &totalFree); err != nil {
		return 0, 0, fmt.Errorf("GetDiskFreeSpaceEx(%s): %w", path, err)
	}
	return callerFree, totalBytes, nil
}

func userInteractive(threshold time.Duration) (bool, error) {
	var info lastInputInfo
	info.Size = uint32(unsafe.Sizeof(info))
	r1, _, callErr := procGetLastInputInfo.Call(uintptr(unsafe.Pointer(&info)))
	if r1 == 0 {
		return false, fmt.Errorf("GetLastInputInfo: %w", callErr)
	}
	now, _, _ := procGetTickCount.Call()
	idleMillis := uint32(now) - info.Time // uint32 subtraction intentionally handles tick wrap.
	return time.Duration(idleMillis)*time.Millisecond < threshold, nil
}

func (s *WindowsSensor) marDiskUsage(ctx context.Context) (uint64, error) {
	now := time.Now()
	s.mu.Lock()
	if s.cfg.DiskUsageCacheTTL > 0 && !s.diskCachedAt.IsZero() && now.Sub(s.diskCachedAt) < s.cfg.DiskUsageCacheTTL {
		cached := s.diskCached
		s.mu.Unlock()
		return cached, nil
	}
	s.mu.Unlock()

	var total uint64
	for _, root := range s.cfg.MARRoots {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		if root == "" {
			continue
		}
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				if os.IsNotExist(err) {
					return nil
				}
				return err
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if d.IsDir() || d.Type()&os.ModeSymlink != 0 {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			if info.Size() > 0 {
				total = saturatingAdd(total, uint64(info.Size()))
			}
			return nil
		})
		if err != nil && !os.IsNotExist(err) {
			return 0, fmt.Errorf("scan MAR disk root %s: %w", root, err)
		}
	}

	s.mu.Lock()
	s.diskCachedAt = now
	s.diskCached = total
	s.mu.Unlock()
	return total, nil
}

func filetimeTicks(ft windows.Filetime) uint64 {
	return uint64(ft.HighDateTime)<<32 | uint64(ft.LowDateTime)
}
