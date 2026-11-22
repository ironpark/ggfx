// Copyright 2026 The ggfx Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:build ((darwin && !ios) || windows || (linux && !android)) && (amd64 || arm64)

package zigdriver

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

const maxSyscallArgs = 7

// syscallArgsPool holds argument buffers for purego.SyscallN. purego.SyscallN
// lets its variadic slice escape, so pooling the fixed-size array keeps every
// steady-state FFI call at 0 allocations.
var syscallArgsPool = sync.Pool{New: func() any { return new([maxSyscallArgs]uintptr) }}

func call0(fn uintptr) uintptr {
	r, _, _ := purego.SyscallN(fn)
	return r
}

//go:uintptrescapes
func call1(fn, a1 uintptr) uintptr {
	return callN(fn, 1, a1, 0, 0, 0, 0, 0, 0)
}

//go:uintptrescapes
func call2(fn, a1, a2 uintptr) uintptr {
	return callN(fn, 2, a1, a2, 0, 0, 0, 0, 0)
}

//go:uintptrescapes
func call3(fn, a1, a2, a3 uintptr) uintptr {
	return callN(fn, 3, a1, a2, a3, 0, 0, 0, 0)
}

//go:uintptrescapes
func call5(fn, a1, a2, a3, a4, a5 uintptr) uintptr {
	return callN(fn, 5, a1, a2, a3, a4, a5, 0, 0)
}

//go:uintptrescapes
func call6(fn, a1, a2, a3, a4, a5, a6 uintptr) uintptr {
	return callN(fn, 6, a1, a2, a3, a4, a5, a6, 0)
}

//go:uintptrescapes
func call7(fn, a1, a2, a3, a4, a5, a6, a7 uintptr) uintptr {
	return callN(fn, 7, a1, a2, a3, a4, a5, a6, a7)
}

func callN(fn uintptr, n int, a1, a2, a3, a4, a5, a6, a7 uintptr) uintptr {
	buf := syscallArgsPool.Get().(*[maxSyscallArgs]uintptr)
	buf[0], buf[1], buf[2], buf[3], buf[4], buf[5], buf[6] = a1, a2, a3, a4, a5, a6, a7
	r, _, _ := purego.SyscallN(fn, buf[:n]...)
	syscallArgsPool.Put(buf)
	return r
}

// library holds resolved symbol addresses for the 14 exported C ABI functions.
// Methods invoke each symbol directly via purego.SyscallN to avoid per-call
// reflection boxing allocations. Every parameter is strictly a pointer or
// integer scalar.
type library struct {
	path   string
	handle uintptr

	fnABIVersion            uintptr
	fnDeviceCreate          uintptr
	fnDeviceDestroy         uintptr
	fnSetMainThreadMode     uintptr
	fnSubmit                uintptr
	fnCreateShader          uintptr
	fnReadPixels            uintptr
	fnFinishForcedFrame     uintptr
	fnPresentOnMainThread   uintptr
	fnIsOccluded            uintptr
	fnSurfaceCreate         uintptr
	fnSurfaceSetWindowState uintptr
	fnSurfaceKept           uintptr
	fnDebugStats            uintptr
}

type libSymbol struct {
	name string
	dst  *uintptr
}

func (l *library) symbols() []libSymbol {
	return []libSymbol{
		{"ggfx_abi_version", &l.fnABIVersion},
		{"ggfx_device_create", &l.fnDeviceCreate},
		{"ggfx_device_destroy", &l.fnDeviceDestroy},
		{"ggfx_device_set_main_thread_mode", &l.fnSetMainThreadMode},
		{"ggfx_submit", &l.fnSubmit},
		{"ggfx_create_shader", &l.fnCreateShader},
		{"ggfx_read_pixels", &l.fnReadPixels},
		{"ggfx_finish_forced_frame", &l.fnFinishForcedFrame},
		{"ggfx_present_on_main_thread", &l.fnPresentOnMainThread},
		{"ggfx_device_is_occluded", &l.fnIsOccluded},
		{"ggfx_surface_create", &l.fnSurfaceCreate},
		{"ggfx_surface_set_window_state", &l.fnSurfaceSetWindowState},
		{"ggfx_surface_kept", &l.fnSurfaceKept},
		{"ggfx_debug_stats", &l.fnDebugStats},
	}
}

func (l *library) abiVersion() uint32 {
	return uint32(call0(l.fnABIVersion))
}

func (l *library) deviceCreate(opts *DeviceOptions, outDev *uintptr, outCaps *DriverCaps, errBuf *byte, errCap uintptr) int32 {
	r := call5(l.fnDeviceCreate,
		uintptr(unsafe.Pointer(opts)),
		uintptr(unsafe.Pointer(outDev)),
		uintptr(unsafe.Pointer(outCaps)),
		uintptr(unsafe.Pointer(errBuf)),
		errCap,
	)
	runtime.KeepAlive(opts)
	runtime.KeepAlive(outDev)
	runtime.KeepAlive(outCaps)
	runtime.KeepAlive(errBuf)
	return int32(r)
}

func (l *library) deviceDestroy(dev uintptr, errBuf *byte, errCap uintptr) int32 {
	r := call3(l.fnDeviceDestroy,
		dev,
		uintptr(unsafe.Pointer(errBuf)),
		errCap,
	)
	runtime.KeepAlive(errBuf)
	return int32(r)
}

func (l *library) setMainThreadMode(dev uintptr, mode uint32) {
	call2(l.fnSetMainThreadMode, dev, uintptr(mode))
}

func (l *library) submit(dev uintptr, batch *Batch, flushMode uint32, outRes *SubmitResult, errBuf *byte, errCap uintptr) int32 {
	r := call6(l.fnSubmit,
		dev,
		uintptr(unsafe.Pointer(batch)),
		uintptr(flushMode),
		uintptr(unsafe.Pointer(outRes)),
		uintptr(unsafe.Pointer(errBuf)),
		errCap,
	)
	runtime.KeepAlive(batch)
	runtime.KeepAlive(outRes)
	runtime.KeepAlive(errBuf)
	return int32(r)
}

func (l *library) createShader(dev uintptr, shaderID uint32, desc *ShaderDesc, errBuf *byte, errCap uintptr) int32 {
	r := call5(l.fnCreateShader,
		dev,
		uintptr(shaderID),
		uintptr(unsafe.Pointer(desc)),
		uintptr(unsafe.Pointer(errBuf)),
		errCap,
	)
	runtime.KeepAlive(desc)
	runtime.KeepAlive(errBuf)
	return int32(r)
}

func (l *library) readPixels(dev uintptr, pending *Batch, imageID uint32, regions *ReadRegion, regionCount uint32, errBuf *byte, errCap uintptr) int32 {
	r := call7(l.fnReadPixels,
		dev,
		uintptr(unsafe.Pointer(pending)),
		uintptr(imageID),
		uintptr(unsafe.Pointer(regions)),
		uintptr(regionCount),
		uintptr(unsafe.Pointer(errBuf)),
		errCap,
	)
	runtime.KeepAlive(pending)
	runtime.KeepAlive(regions)
	runtime.KeepAlive(errBuf)
	return int32(r)
}

func (l *library) finishForcedFrame(dev uintptr, errBuf *byte, errCap uintptr) int32 {
	r := call3(l.fnFinishForcedFrame,
		dev,
		uintptr(unsafe.Pointer(errBuf)),
		errCap,
	)
	runtime.KeepAlive(errBuf)
	return int32(r)
}

func (l *library) presentOnMainThread(dev uintptr, errBuf *byte, errCap uintptr) int32 {
	r := call3(l.fnPresentOnMainThread,
		dev,
		uintptr(unsafe.Pointer(errBuf)),
		errCap,
	)
	runtime.KeepAlive(errBuf)
	return int32(r)
}

func (l *library) isOccluded(dev uintptr) uint32 {
	return uint32(call1(l.fnIsOccluded, dev))
}

func (l *library) surfaceCreate(dev uintptr, surfaceID uint32, desc *SurfaceDesc, outCaps *SurfaceCaps, errBuf *byte, errCap uintptr) int32 {
	r := call6(l.fnSurfaceCreate,
		dev,
		uintptr(surfaceID),
		uintptr(unsafe.Pointer(desc)),
		uintptr(unsafe.Pointer(outCaps)),
		uintptr(unsafe.Pointer(errBuf)),
		errCap,
	)
	runtime.KeepAlive(desc)
	runtime.KeepAlive(outCaps)
	runtime.KeepAlive(errBuf)
	return int32(r)
}

func (l *library) surfaceSetWindowState(dev uintptr, surfaceID uint32, stateFlags uint32) {
	call3(l.fnSurfaceSetWindowState, dev, uintptr(surfaceID), uintptr(stateFlags))
}

func (l *library) surfaceKept(dev uintptr, surfaceID uint32) uint32 {
	return uint32(call2(l.fnSurfaceKept, dev, uintptr(surfaceID)))
}

func (l *library) debugStats(dev uintptr, out *DebugStats) int32 {
	r := call2(l.fnDebugStats, dev, uintptr(unsafe.Pointer(out)))
	runtime.KeepAlive(out)
	return int32(r)
}

func libFileName() string {
	switch runtime.GOOS {
	case "darwin":
		return "libggfx_driver.dylib"
	case "windows":
		return "ggfx_driver.dll"
	default:
		return "libggfx_driver.so"
	}
}

var (
	libCacheMu sync.Mutex
	libCache   = map[string]*library{}
)

// loadLibrary loads and validates the Zig driver shared library following the search order:
//  1. pathOverride (or GGFX_DRIVER_LIB if pathOverride is empty) — failure returns immediately.
//  2. Adjacent to the executable (plus ../Frameworks/libggfx_driver.dylib on macOS).
//  3. System library search path by filename.
//
// Successful loads are cached by resolved key; failed lookups are not cached so tests can
// switch GGFX_DRIVER_LIB or test fallback paths cleanly.
func loadLibrary(pathOverride string) (*library, error) {
	explicit := pathOverride
	if explicit == "" {
		explicit = os.Getenv("GGFX_DRIVER_LIB")
	}

	if explicit != "" {
		return loadExplicitLibrary(explicit)
	}
	return loadDefaultLibrary()
}

func loadExplicitLibrary(path string) (*library, error) {
	libCacheMu.Lock()
	if cached, ok := libCache[path]; ok {
		libCacheMu.Unlock()
		return cached, nil
	}
	libCacheMu.Unlock()

	lib, err := openAndVerifyLibrary(path)
	if err != nil {
		return nil, err
	}

	libCacheMu.Lock()
	if existing, ok := libCache[path]; ok {
		libCacheMu.Unlock()
		closeLibrary(lib.handle)
		return existing, nil
	}
	libCache[path] = lib
	libCacheMu.Unlock()
	return lib, nil
}

func loadDefaultLibrary() (*library, error) {
	const autoKey = ""

	libCacheMu.Lock()
	if cached, ok := libCache[autoKey]; ok {
		libCacheMu.Unlock()
		return cached, nil
	}
	libCacheMu.Unlock()

	name := libFileName()
	candidates := make([]string, 0, 3)
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		candidates = append(candidates, filepath.Join(dir, name))
		if runtime.GOOS == "darwin" {
			candidates = append(candidates, filepath.Join(dir, "..", "Frameworks", name))
		}
	}
	candidates = append(candidates, name)

	var lastErr error
	for _, cand := range candidates {
		lib, err := openAndVerifyLibrary(cand)
		if err == nil {
			libCacheMu.Lock()
			if existing, ok := libCache[autoKey]; ok {
				libCacheMu.Unlock()
				closeLibrary(lib.handle)
				return existing, nil
			}
			libCache[autoKey] = lib
			libCacheMu.Unlock()
			return lib, nil
		}
		if errIsABIMismatch(err) {
			return nil, err
		}
		lastErr = err
	}
	return nil, fmt.Errorf("%w: could not load %s (%v)", ErrBackendUnavailable, name, lastErr)
}

func errIsABIMismatch(err error) bool {
	if err == nil {
		return false
	}
	type unwrapper interface{ Unwrap() error }
	for e := err; e != nil; {
		if e == ErrABIMismatch {
			return true
		}
		u, ok := e.(unwrapper)
		if !ok {
			return false
		}
		e = u.Unwrap()
	}
	return false
}

func openAndVerifyLibrary(path string) (*library, error) {
	lib, err := openLibrary(path)
	if err != nil {
		return nil, fmt.Errorf("%w: open %q: %v", ErrBackendUnavailable, path, err)
	}
	if ver := lib.abiVersion(); ver != DriverABIVersion {
		closeLibrary(lib.handle)
		return nil, fmt.Errorf("%w: library %q reports ABI version %d, want %d", ErrABIMismatch, path, ver, DriverABIVersion)
	}
	return lib, nil
}
