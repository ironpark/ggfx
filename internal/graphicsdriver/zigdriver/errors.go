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
	"bytes"
	"errors"
	"fmt"
)

// Sentinel errors corresponding to native C ABI status codes and Go driver lifecycle states.
var (
	ErrInvalidArgument    = errors.New("zigdriver: invalid argument")
	ErrABIMismatch        = errors.New("zigdriver: ABI version mismatch")
	ErrBackendUnavailable = errors.New("zigdriver: backend unavailable")
	ErrOutOfMemory        = errors.New("zigdriver: out of memory")
	ErrDeviceLost         = errors.New("zigdriver: device lost")
	ErrUnknownHandle      = errors.New("zigdriver: unknown handle")
	ErrDuplicateHandle    = errors.New("zigdriver: duplicate handle")
	ErrShaderCompile      = errors.New("zigdriver: shader compile failed")
	ErrUnsupported        = errors.New("zigdriver: unsupported")
	ErrMalformedBatch     = errors.New("zigdriver: malformed batch")
	ErrSurfaceLost        = errors.New("zigdriver: surface lost")
	ErrInternal           = errors.New("zigdriver: internal error")
	ErrLeakDetected       = errors.New("zigdriver: memory leak detected")
	ErrClosed             = errors.New("zigdriver: graphics device is closed")
)

type statusError struct {
	op   string
	code int32
	msg  string
}

func (e *statusError) Error() string {
	if e.msg != "" {
		return fmt.Sprintf("zigdriver: %s: %s (status=%d)", e.op, e.msg, e.code)
	}
	if s := statusSentinel(e.code); s != nil {
		return fmt.Sprintf("%s: %v (status=%d)", e.op, s, e.code)
	}
	return fmt.Sprintf("zigdriver: %s: status=%d", e.op, e.code)
}

func (e *statusError) Unwrap() error {
	return statusSentinel(e.code)
}

func statusSentinel(code int32) error {
	switch code {
	case StatusErrInvalidArgument:
		return ErrInvalidArgument
	case StatusErrABIMismatch:
		return ErrABIMismatch
	case StatusErrBackendUnavailable:
		return ErrBackendUnavailable
	case StatusErrOutOfMemory:
		return ErrOutOfMemory
	case StatusErrDeviceLost:
		return ErrDeviceLost
	case StatusErrUnknownHandle:
		return ErrUnknownHandle
	case StatusErrDuplicateHandle:
		return ErrDuplicateHandle
	case StatusErrShaderCompile:
		return ErrShaderCompile
	case StatusErrUnsupported:
		return ErrUnsupported
	case StatusErrMalformedBatch:
		return ErrMalformedBatch
	case StatusErrSurfaceLost:
		return ErrSurfaceLost
	case StatusErrInternal:
		return ErrInternal
	case StatusErrLeakDetected:
		return ErrLeakDetected
	default:
		return nil
	}
}

func errBufToString(buf []byte) string {
	if len(buf) == 0 {
		return ""
	}
	if i := bytes.IndexByte(buf, 0); i >= 0 {
		return string(buf[:i])
	}
	return string(buf)
}

func statusToError(op string, code int32, errBuf []byte) error {
	if code == StatusOK {
		return nil
	}
	return &statusError{
		op:   op,
		code: code,
		msg:  errBufToString(errBuf),
	}
}
