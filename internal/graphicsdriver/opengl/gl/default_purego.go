// Copyright 2022 The Ebitengine Authors
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

//go:build darwin || freebsd || linux || netbsd || windows

package gl

import (
	"runtime"
	"unsafe"

	"github.com/ebitengine/purego"
)

type defaultContext struct {
	gpActiveTexture           uintptr
	gpAttachShader            uintptr
	gpBindBuffer              uintptr
	gpBindBufferBase          uintptr
	gpGetUniformBlockIndex    uintptr
	gpUniformBlockBinding     uintptr
	gpBindFramebuffer         uintptr
	gpBindTexture             uintptr
	gpBindVertexArray         uintptr
	gpBlendEquationSeparate   uintptr
	gpBlendFuncSeparate       uintptr
	gpBlitFramebuffer         uintptr
	gpBufferData              uintptr
	gpBufferSubData           uintptr
	gpCheckFramebufferStatus  uintptr
	gpCompileShader           uintptr
	gpCreateProgram           uintptr
	gpCreateShader            uintptr
	gpDeleteBuffers           uintptr
	gpDeleteFramebuffers      uintptr
	gpDeleteProgram           uintptr
	gpDeleteShader            uintptr
	gpDeleteTextures          uintptr
	gpDeleteVertexArrays      uintptr
	gpDrawElements            uintptr
	gpEnable                  uintptr
	gpEnableVertexAttribArray uintptr
	gpFinish                  uintptr
	gpFlush                   uintptr
	gpFramebufferTexture2D    uintptr
	gpGenBuffers              uintptr
	gpGenFramebuffers         uintptr
	gpGenTextures             uintptr
	gpGenVertexArrays         uintptr
	gpGetError                uintptr
	gpGetIntegerv             uintptr
	gpGetProgramInfoLog       uintptr
	gpGetProgramiv            uintptr
	gpGetShaderInfoLog        uintptr
	gpGetShaderiv             uintptr
	gpGetUniformLocation      uintptr
	gpIsProgram               uintptr
	gpLinkProgram             uintptr
	gpPixelStorei             uintptr
	gpReadPixels              uintptr
	gpScissor                 uintptr
	gpShaderSource            uintptr
	gpTexImage2D              uintptr
	gpTexParameteri           uintptr
	gpTexSubImage2D           uintptr
	gpUniform1i               uintptr
	gpUseProgram              uintptr
	gpVertexAttribPointer     uintptr
	gpViewport                uintptr

	isES bool

	// syscallArgs holds the arguments of a purego.SyscallN call. purego.SyscallN lets its
	// variadic slice escape, so reusing an array owned by defaultContext keeps GL calls from
	// allocating. OpenGL calls are serialized on one thread.
	syscallArgs [10]uintptr
	tmpUint32   uint32
	tmpInt32    int32
}

func (c *defaultContext) call0(fn uintptr) uintptr {
	r, _, _ := purego.SyscallN(fn)
	return r
}

//go:uintptrescapes
func (c *defaultContext) call1(fn, a1 uintptr) uintptr {
	c.syscallArgs[0] = a1
	r, _, _ := purego.SyscallN(fn, c.syscallArgs[:1]...)
	return r
}

//go:uintptrescapes
func (c *defaultContext) call2(fn, a1, a2 uintptr) uintptr {
	c.syscallArgs[0], c.syscallArgs[1] = a1, a2
	r, _, _ := purego.SyscallN(fn, c.syscallArgs[:2]...)
	return r
}

//go:uintptrescapes
func (c *defaultContext) call3(fn, a1, a2, a3 uintptr) uintptr {
	c.syscallArgs[0], c.syscallArgs[1], c.syscallArgs[2] = a1, a2, a3
	r, _, _ := purego.SyscallN(fn, c.syscallArgs[:3]...)
	return r
}

//go:uintptrescapes
func (c *defaultContext) call4(fn, a1, a2, a3, a4 uintptr) uintptr {
	c.syscallArgs[0], c.syscallArgs[1], c.syscallArgs[2], c.syscallArgs[3] = a1, a2, a3, a4
	r, _, _ := purego.SyscallN(fn, c.syscallArgs[:4]...)
	return r
}

//go:uintptrescapes
func (c *defaultContext) call5(fn, a1, a2, a3, a4, a5 uintptr) uintptr {
	c.syscallArgs[0], c.syscallArgs[1], c.syscallArgs[2], c.syscallArgs[3], c.syscallArgs[4] = a1, a2, a3, a4, a5
	r, _, _ := purego.SyscallN(fn, c.syscallArgs[:5]...)
	return r
}

//go:uintptrescapes
func (c *defaultContext) call6(fn, a1, a2, a3, a4, a5, a6 uintptr) uintptr {
	c.syscallArgs[0], c.syscallArgs[1], c.syscallArgs[2], c.syscallArgs[3], c.syscallArgs[4], c.syscallArgs[5] = a1, a2, a3, a4, a5, a6
	r, _, _ := purego.SyscallN(fn, c.syscallArgs[:6]...)
	return r
}

//go:uintptrescapes
func (c *defaultContext) call7(fn, a1, a2, a3, a4, a5, a6, a7 uintptr) uintptr {
	c.syscallArgs[0], c.syscallArgs[1], c.syscallArgs[2], c.syscallArgs[3], c.syscallArgs[4], c.syscallArgs[5], c.syscallArgs[6] = a1, a2, a3, a4, a5, a6, a7
	r, _, _ := purego.SyscallN(fn, c.syscallArgs[:7]...)
	return r
}

//go:uintptrescapes
func (c *defaultContext) call9(fn, a1, a2, a3, a4, a5, a6, a7, a8, a9 uintptr) uintptr {
	c.syscallArgs[0], c.syscallArgs[1], c.syscallArgs[2], c.syscallArgs[3], c.syscallArgs[4] = a1, a2, a3, a4, a5
	c.syscallArgs[5], c.syscallArgs[6], c.syscallArgs[7], c.syscallArgs[8] = a6, a7, a8, a9
	r, _, _ := purego.SyscallN(fn, c.syscallArgs[:9]...)
	return r
}

//go:uintptrescapes
func (c *defaultContext) call10(fn, a1, a2, a3, a4, a5, a6, a7, a8, a9, a10 uintptr) uintptr {
	c.syscallArgs[0], c.syscallArgs[1], c.syscallArgs[2], c.syscallArgs[3], c.syscallArgs[4] = a1, a2, a3, a4, a5
	c.syscallArgs[5], c.syscallArgs[6], c.syscallArgs[7], c.syscallArgs[8], c.syscallArgs[9] = a6, a7, a8, a9, a10
	r, _, _ := purego.SyscallN(fn, c.syscallArgs[:10]...)
	return r
}

func NewDefaultContext() (Context, error) {
	ctx := &defaultContext{}
	if err := ctx.init(); err != nil {
		return nil, err
	}
	return ctx, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (c *defaultContext) IsES() bool {
	return c.isES
}

func (c *defaultContext) ActiveTexture(texture uint32) {
	c.call1(c.gpActiveTexture, uintptr(texture))
}

func (c *defaultContext) AttachShader(program uint32, shader uint32) {
	c.call2(c.gpAttachShader, uintptr(program), uintptr(shader))
}

func (c *defaultContext) BindBuffer(target uint32, buffer uint32) {
	c.call2(c.gpBindBuffer, uintptr(target), uintptr(buffer))
}

func (c *defaultContext) BindBufferBase(target uint32, index uint32, buffer uint32) {
	c.call3(c.gpBindBufferBase, uintptr(target), uintptr(index), uintptr(buffer))
}

func (c *defaultContext) GetUniformBlockIndex(program uint32, name string) uint32 {
	cname, free := cStr(name)
	defer free()
	ret := c.call2(c.gpGetUniformBlockIndex, uintptr(program), uintptr(unsafe.Pointer(cname)))
	return uint32(ret)
}

func (c *defaultContext) UniformBlockBinding(program uint32, index uint32, binding uint32) {
	c.call3(c.gpUniformBlockBinding, uintptr(program), uintptr(index), uintptr(binding))
}

func (c *defaultContext) BindFramebuffer(target uint32, framebuffer uint32) {
	c.call2(c.gpBindFramebuffer, uintptr(target), uintptr(framebuffer))
}

func (c *defaultContext) BindTexture(target uint32, texture uint32) {
	c.call2(c.gpBindTexture, uintptr(target), uintptr(texture))
}

func (c *defaultContext) BindVertexArray(array uint32) {
	c.call1(c.gpBindVertexArray, uintptr(array))
}

func (c *defaultContext) BlendEquationSeparate(modeRGB uint32, modeAlpha uint32) {
	c.call2(c.gpBlendEquationSeparate, uintptr(modeRGB), uintptr(modeAlpha))
}

func (c *defaultContext) BlendFuncSeparate(srcRGB uint32, dstRGB uint32, srcAlpha uint32, dstAlpha uint32) {
	c.call4(c.gpBlendFuncSeparate, uintptr(srcRGB), uintptr(dstRGB), uintptr(srcAlpha), uintptr(dstAlpha))
}

func (c *defaultContext) BlitFramebuffer(srcX0, srcY0, srcX1, srcY1, dstX0, dstY0, dstX1, dstY1 int32, mask uint32, filter uint32) {
	// mask and filter are past the eight register arguments. Apple's arm64 ABI packs arguments on the
	// stack at their own size rather than in 8-byte slots, so there the two share one slot.
	if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" {
		c.call9(c.gpBlitFramebuffer, uintptr(srcX0), uintptr(srcY0), uintptr(srcX1), uintptr(srcY1), uintptr(dstX0), uintptr(dstY0), uintptr(dstX1), uintptr(dstY1), uintptr(uint64(mask)|uint64(filter)<<32))
		return
	}
	c.call10(c.gpBlitFramebuffer, uintptr(srcX0), uintptr(srcY0), uintptr(srcX1), uintptr(srcY1), uintptr(dstX0), uintptr(dstY0), uintptr(dstX1), uintptr(dstY1), uintptr(mask), uintptr(filter))
}

func (c *defaultContext) BufferInit(target uint32, size int, usage uint32) {
	c.call4(c.gpBufferData, uintptr(target), uintptr(size), 0, uintptr(usage))
}

func (c *defaultContext) BufferSubData(target uint32, offset int, data []byte) {
	if len(data) == 0 {
		return
	}
	c.call4(c.gpBufferSubData, uintptr(target), uintptr(offset), uintptr(len(data)), uintptr(unsafe.Pointer(&data[0])))
	runtime.KeepAlive(data)
}

func (c *defaultContext) CheckFramebufferStatus(target uint32) uint32 {
	return uint32(c.call1(c.gpCheckFramebufferStatus, uintptr(target)))
}

func (c *defaultContext) CompileShader(shader uint32) {
	c.call1(c.gpCompileShader, uintptr(shader))
}

func (c *defaultContext) CreateBuffer() uint32 {
	c.tmpUint32 = 0
	c.call2(c.gpGenBuffers, 1, uintptr(unsafe.Pointer(&c.tmpUint32)))
	return c.tmpUint32
}

func (c *defaultContext) CreateFramebuffer() uint32 {
	c.tmpUint32 = 0
	c.call2(c.gpGenFramebuffers, 1, uintptr(unsafe.Pointer(&c.tmpUint32)))
	return c.tmpUint32
}

func (c *defaultContext) CreateProgram() uint32 {
	return uint32(c.call0(c.gpCreateProgram))
}

func (c *defaultContext) CreateShader(xtype uint32) uint32 {
	return uint32(c.call1(c.gpCreateShader, uintptr(xtype)))
}

func (c *defaultContext) CreateTexture() uint32 {
	c.tmpUint32 = 0
	c.call2(c.gpGenTextures, 1, uintptr(unsafe.Pointer(&c.tmpUint32)))
	return c.tmpUint32
}

func (c *defaultContext) CreateVertexArray() uint32 {
	c.tmpUint32 = 0
	c.call2(c.gpGenVertexArrays, 1, uintptr(unsafe.Pointer(&c.tmpUint32)))
	return c.tmpUint32
}

func (c *defaultContext) DeleteBuffer(buffer uint32) {
	c.tmpUint32 = buffer
	c.call2(c.gpDeleteBuffers, 1, uintptr(unsafe.Pointer(&c.tmpUint32)))
}

func (c *defaultContext) DeleteFramebuffer(framebuffer uint32) {
	c.tmpUint32 = framebuffer
	c.call2(c.gpDeleteFramebuffers, 1, uintptr(unsafe.Pointer(&c.tmpUint32)))
}

func (c *defaultContext) DeleteProgram(program uint32) {
	// A program is no longer valid after a context loss and must not be deleted.
	if !c.isProgram(program) {
		return
	}
	c.call1(c.gpDeleteProgram, uintptr(program))
}

func (c *defaultContext) DeleteShader(shader uint32) {
	c.call1(c.gpDeleteShader, uintptr(shader))
}

func (c *defaultContext) DeleteTexture(texture uint32) {
	c.tmpUint32 = texture
	c.call2(c.gpDeleteTextures, 1, uintptr(unsafe.Pointer(&c.tmpUint32)))
}

func (c *defaultContext) DeleteVertexArray(array uint32) {
	c.tmpUint32 = array
	c.call2(c.gpDeleteVertexArrays, 1, uintptr(unsafe.Pointer(&c.tmpUint32)))
}

func (c *defaultContext) DrawElements(mode uint32, count int32, xtype uint32, offset int) {
	c.call4(c.gpDrawElements, uintptr(mode), uintptr(count), uintptr(xtype), uintptr(offset))
}

func (c *defaultContext) Enable(cap uint32) {
	c.call1(c.gpEnable, uintptr(cap))
}

func (c *defaultContext) EnableVertexAttribArray(index uint32) {
	c.call1(c.gpEnableVertexAttribArray, uintptr(index))
}

func (c *defaultContext) Finish() {
	c.call0(c.gpFinish)
}

func (c *defaultContext) Flush() {
	c.call0(c.gpFlush)
}

func (c *defaultContext) FramebufferTexture2D(target uint32, attachment uint32, textarget uint32, texture uint32, level int32) {
	c.call5(c.gpFramebufferTexture2D, uintptr(target), uintptr(attachment), uintptr(textarget), uintptr(texture), uintptr(level))
}

func (c *defaultContext) GetError() uint32 {
	return uint32(c.call0(c.gpGetError))
}

func (c *defaultContext) GetExtension(name string) any {
	return nil
}

func (c *defaultContext) GetInteger(pname uint32) int {
	c.tmpInt32 = 0
	c.call2(c.gpGetIntegerv, uintptr(pname), uintptr(unsafe.Pointer(&c.tmpInt32)))
	return int(c.tmpInt32)
}

func (c *defaultContext) GetProgramInfoLog(program uint32) string {
	bufSize := c.GetProgrami(program, INFO_LOG_LENGTH)
	if bufSize <= 0 {
		return ""
	}
	infoLog := make([]byte, bufSize)
	c.tmpInt32 = 0
	c.call4(c.gpGetProgramInfoLog, uintptr(program), uintptr(bufSize), uintptr(unsafe.Pointer(&c.tmpInt32)), uintptr(unsafe.Pointer(&infoLog[0])))
	runtime.KeepAlive(infoLog)
	written := c.tmpInt32
	if written < 0 || int(written) > bufSize {
		written = int32(bufSize)
	}
	return string(infoLog[:written])
}

func (c *defaultContext) GetProgrami(program uint32, pname uint32) int {
	c.tmpInt32 = 0
	c.call3(c.gpGetProgramiv, uintptr(program), uintptr(pname), uintptr(unsafe.Pointer(&c.tmpInt32)))
	return int(c.tmpInt32)
}

func (c *defaultContext) GetShaderInfoLog(shader uint32) string {
	bufSize := c.getShaderi(shader, INFO_LOG_LENGTH)
	if bufSize <= 0 {
		return ""
	}
	infoLog := make([]byte, bufSize)
	c.tmpInt32 = 0
	c.call4(c.gpGetShaderInfoLog, uintptr(shader), uintptr(bufSize), uintptr(unsafe.Pointer(&c.tmpInt32)), uintptr(unsafe.Pointer(&infoLog[0])))
	runtime.KeepAlive(infoLog)
	written := c.tmpInt32
	if written < 0 || int(written) > bufSize {
		written = int32(bufSize)
	}
	return string(infoLog[:written])
}

func (c *defaultContext) getShaderi(shader uint32, pname uint32) int {
	c.tmpInt32 = 0
	c.call3(c.gpGetShaderiv, uintptr(shader), uintptr(pname), uintptr(unsafe.Pointer(&c.tmpInt32)))
	return int(c.tmpInt32)
}

func (c *defaultContext) GetUniformLocation(program uint32, name string) int32 {
	cname, free := cStr(name)
	defer free()
	ret := c.call2(c.gpGetUniformLocation, uintptr(program), uintptr(unsafe.Pointer(cname)))
	return int32(ret)
}

func (c *defaultContext) isProgram(program uint32) bool {
	return byte(c.call1(c.gpIsProgram, uintptr(program))) != 0
}

func (c *defaultContext) LinkProgram(program uint32) {
	c.call1(c.gpLinkProgram, uintptr(program))
}

func (c *defaultContext) PixelStorei(pname uint32, param int32) {
	c.call2(c.gpPixelStorei, uintptr(pname), uintptr(param))
}

func (c *defaultContext) ReadPixels(dst []byte, x int32, y int32, width int32, height int32, format uint32, xtype uint32) {
	if len(dst) == 0 || width <= 0 || height <= 0 {
		return
	}
	c.call7(c.gpReadPixels, uintptr(x), uintptr(y), uintptr(width), uintptr(height), uintptr(format), uintptr(xtype), uintptr(unsafe.Pointer(&dst[0])))
	runtime.KeepAlive(dst)
}

func (c *defaultContext) Scissor(x int32, y int32, width int32, height int32) {
	c.call4(c.gpScissor, uintptr(x), uintptr(y), uintptr(width), uintptr(height))
}

func (c *defaultContext) ShaderSource(shader uint32, xstring string) {
	cstring, free := cStr(xstring)
	defer free()
	c.call4(c.gpShaderSource, uintptr(shader), 1, uintptr(unsafe.Pointer(&cstring)), 0)
}

func (c *defaultContext) TexImage2D(target uint32, level int32, internalformat int32, width int32, height int32, format uint32, xtype uint32, pixels []byte) {
	var ptr *byte
	if len(pixels) > 0 {
		ptr = &pixels[0]
	}
	c.call9(c.gpTexImage2D, uintptr(target), uintptr(level), uintptr(internalformat), uintptr(width), uintptr(height), 0, uintptr(format), uintptr(xtype), uintptr(unsafe.Pointer(ptr)))
	runtime.KeepAlive(pixels)
}

func (c *defaultContext) TexParameteri(target uint32, pname uint32, param int32) {
	c.call3(c.gpTexParameteri, uintptr(target), uintptr(pname), uintptr(param))
}

func (c *defaultContext) TexSubImage2D(target uint32, level int32, xoffset int32, yoffset int32, width int32, height int32, format uint32, xtype uint32, pixels []byte) {
	if len(pixels) == 0 || width <= 0 || height <= 0 {
		return
	}
	c.call9(c.gpTexSubImage2D, uintptr(target), uintptr(level), uintptr(xoffset), uintptr(yoffset), uintptr(width), uintptr(height), uintptr(format), uintptr(xtype), uintptr(unsafe.Pointer(&pixels[0])))
	runtime.KeepAlive(pixels)
}

func (c *defaultContext) Uniform1i(location int32, v0 int32) {
	c.call2(c.gpUniform1i, uintptr(location), uintptr(v0))
}

func (c *defaultContext) UseProgram(program uint32) {
	c.call1(c.gpUseProgram, uintptr(program))
}

func (c *defaultContext) VertexAttribPointer(index uint32, size int32, xtype uint32, normalized bool, stride int32, offset int) {
	c.call6(c.gpVertexAttribPointer, uintptr(index), uintptr(size), uintptr(xtype), uintptr(boolToInt(normalized)), uintptr(stride), uintptr(offset))
}

func (c *defaultContext) Viewport(x int32, y int32, width int32, height int32) {
	c.call4(c.gpViewport, uintptr(x), uintptr(y), uintptr(width), uintptr(height))
}

func (c *defaultContext) LoadFunctions() error {
	g := procAddressGetter{ctx: c}

	c.gpActiveTexture = g.get("glActiveTexture")
	c.gpAttachShader = g.get("glAttachShader")
	c.gpBindBuffer = g.get("glBindBuffer")
	c.gpBindBufferBase = g.get("glBindBufferBase")
	c.gpGetUniformBlockIndex = g.get("glGetUniformBlockIndex")
	c.gpUniformBlockBinding = g.get("glUniformBlockBinding")
	c.gpBindFramebuffer = g.get("glBindFramebuffer")
	c.gpBindTexture = g.get("glBindTexture")
	c.gpBindVertexArray = g.get("glBindVertexArray")
	c.gpBlendEquationSeparate = g.get("glBlendEquationSeparate")
	c.gpBlendFuncSeparate = g.get("glBlendFuncSeparate")
	c.gpBlitFramebuffer = g.get("glBlitFramebuffer")
	c.gpBufferData = g.get("glBufferData")
	c.gpBufferSubData = g.get("glBufferSubData")
	c.gpCheckFramebufferStatus = g.get("glCheckFramebufferStatus")
	c.gpCompileShader = g.get("glCompileShader")
	c.gpCreateProgram = g.get("glCreateProgram")
	c.gpCreateShader = g.get("glCreateShader")
	c.gpDeleteBuffers = g.get("glDeleteBuffers")
	c.gpDeleteFramebuffers = g.get("glDeleteFramebuffers")
	c.gpDeleteProgram = g.get("glDeleteProgram")
	c.gpDeleteShader = g.get("glDeleteShader")
	c.gpDeleteTextures = g.get("glDeleteTextures")
	c.gpDeleteVertexArrays = g.get("glDeleteVertexArrays")
	c.gpDrawElements = g.get("glDrawElements")
	c.gpEnable = g.get("glEnable")
	c.gpEnableVertexAttribArray = g.get("glEnableVertexAttribArray")
	c.gpFinish = g.get("glFinish")
	c.gpFlush = g.get("glFlush")
	c.gpFramebufferTexture2D = g.get("glFramebufferTexture2D")
	c.gpGenBuffers = g.get("glGenBuffers")
	c.gpGenFramebuffers = g.get("glGenFramebuffers")
	c.gpGenTextures = g.get("glGenTextures")
	c.gpGenVertexArrays = g.get("glGenVertexArrays")
	c.gpGetError = g.get("glGetError")
	c.gpGetIntegerv = g.get("glGetIntegerv")
	c.gpGetProgramInfoLog = g.get("glGetProgramInfoLog")
	c.gpGetProgramiv = g.get("glGetProgramiv")
	c.gpGetShaderInfoLog = g.get("glGetShaderInfoLog")
	c.gpGetShaderiv = g.get("glGetShaderiv")
	c.gpGetUniformLocation = g.get("glGetUniformLocation")
	c.gpIsProgram = g.get("glIsProgram")
	c.gpLinkProgram = g.get("glLinkProgram")
	c.gpPixelStorei = g.get("glPixelStorei")
	c.gpReadPixels = g.get("glReadPixels")
	c.gpScissor = g.get("glScissor")
	c.gpShaderSource = g.get("glShaderSource")
	c.gpTexImage2D = g.get("glTexImage2D")
	c.gpTexParameteri = g.get("glTexParameteri")
	c.gpTexSubImage2D = g.get("glTexSubImage2D")
	c.gpUniform1i = g.get("glUniform1i")
	c.gpUseProgram = g.get("glUseProgram")
	c.gpVertexAttribPointer = g.get("glVertexAttribPointer")
	c.gpViewport = g.get("glViewport")

	return g.error()
}

// cStr takes a Go string (with or without null-termination)
// and returns the C counterpart.
//
// The returned free function must be called once you are done using the string
// in order to free the memory.
func cStr(str string) (cstr *byte, free func()) {
	bs := []byte(str)
	if len(bs) == 0 || bs[len(bs)-1] != 0 {
		bs = append(bs, 0)
	}
	return &bs[0], func() {
		runtime.KeepAlive(bs)
		bs = nil
	}
}
