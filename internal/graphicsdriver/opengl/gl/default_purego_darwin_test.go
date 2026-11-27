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

//go:build darwin && !race

package gl

import (
	"testing"

	"github.com/ebitengine/purego"
)

func newNoopPuregoContext(tb testing.TB) *defaultContext {
	tb.Helper()
	// abs(int) reads only the first integer register and has no side effects, so it can stand in
	// for GL entry points of any arity when testing syscall argument marshaling allocations.
	fn, err := purego.Dlsym(purego.RTLD_DEFAULT, "abs")
	if err != nil {
		tb.Fatal(err)
	}
	return &defaultContext{
		gpActiveTexture:         fn,
		gpBindBuffer:            fn,
		gpBindBufferBase:        fn,
		gpBindFramebuffer:       fn,
		gpBindTexture:           fn,
		gpBindVertexArray:       fn,
		gpBlendEquationSeparate: fn,
		gpBlendFuncSeparate:     fn,
		gpBlitFramebuffer:       fn,
		gpBufferData:            fn,
		gpBufferSubData:         fn,
		gpDrawElements:          fn,
		gpFlush:                 fn,
		gpScissor:               fn,
		gpUniform1i:             fn,
		gpUseProgram:            fn,
		gpViewport:              fn,
	}
}

func TestPuregoPerDrawZeroAllocs(t *testing.T) {
	c := newNoopPuregoContext(t)
	buf := make([]byte, 64)

	allocs := testing.AllocsPerRun(100, func() {
		c.UseProgram(1)
		c.ActiveTexture(TEXTURE0)
		c.BindTexture(TEXTURE_2D, 2)
		c.BindFramebuffer(FRAMEBUFFER, 3)
		c.BindVertexArray(4)
		c.BindBuffer(UNIFORM_BUFFER, 5)
		c.BindBufferBase(UNIFORM_BUFFER, 1, 5)
		c.BufferInit(UNIFORM_BUFFER, len(buf), DYNAMIC_DRAW)
		c.BufferSubData(UNIFORM_BUFFER, 0, buf)
		c.BlendFuncSeparate(ONE, ONE_MINUS_SRC_ALPHA, ONE, ONE_MINUS_SRC_ALPHA)
		c.BlendEquationSeparate(FUNC_ADD, FUNC_ADD)
		c.Uniform1i(0, 0)
		c.Viewport(0, 0, 256, 256)
		c.Scissor(0, 0, 16, 16)
		c.DrawElements(TRIANGLES, 6, UNSIGNED_INT, 0)
		c.BlitFramebuffer(0, 0, 16, 16, 0, 16, 16, 0, COLOR_BUFFER_BIT, NEAREST)
		c.Flush()
	})
	if allocs != 0 {
		t.Fatalf("per-draw purego GL calls allocated %.1f times/run, want 0", allocs)
	}
}
