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

package remote

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"math"
	"sync"
	"sync/atomic"

	"github.com/ironpark/ggfx/internal/graphics"
	"github.com/ironpark/ggfx/internal/graphicsdriver"
	"github.com/ironpark/ggfx/internal/shader"
)

// Target is the viewer's image that the app's screen draws go to: an offscreen image of the
// driver's whose top-left Width by Height pixels are the screen, and which draws at its internal
// size.
type Target struct {
	Image                         graphicsdriver.Image
	Width, Height                 int
	InternalWidth, InternalHeight int
}

// Result is what a batch did.
type Result struct {
	// Presented reports that the batch ended with a present of the screen.
	Presented bool
	// Damage is what the present changed, in the screen's pixels; nil is all of it.
	Damage []image.Rectangle
	// Draws counts the draw calls the batch made.
	Draws int
}

type replayedImage struct {
	img           graphicsdriver.Image // nil for the screen, which is the replay's target
	width, height int
	format        graphicsdriver.PixelFormat
	screen        bool
}

// bounds returns the pixels of i a driver call may touch: those of its texture, which is of the
// image's internal size, but for the screen, which is the target's top-left width by height.
func (i *replayedImage) bounds() image.Rectangle {
	if i.screen {
		return image.Rect(0, 0, i.width, i.height)
	}
	return image.Rect(0, 0, graphics.InternalImageSize(i.width), graphics.InternalImageSize(i.height))
}

type replayedShader struct {
	shader  graphicsdriver.Shader
	program *shader.Program
}

// Replayer runs one app's batches on the viewer's driver. Each app gets its own: the ids are the
// app's, and Dispose frees what one app made when it goes away. Its methods run on the render
// thread, inside a command batch of the viewer's.
type Replayer struct {
	images  map[uint32]*replayedImage
	shaders map[uint32]*replayedShader

	// salt keeps this app's uniform versions apart from the viewer's own, which count up from 1,
	// and from every other app's: the driver skips uploading a block whose version it uploaded
	// last.
	salt uint64

	// vertices and indices are the counts the last SetVertices gave, which draws are checked
	// against.
	vertices, indices int
}

// The versions an app gives are below 1<<versionBits; the replay puts its salt above them.
const versionBits = 48

var nextSalt atomic.Uint64

// NewReplayer returns the replayer of a new app.
func NewReplayer() *Replayer {
	return &Replayer{
		images:  map[uint32]*replayedImage{},
		shaders: map[uint32]*replayedShader{},
		salt:    nextSalt.Add(1) % (1 << 13) << versionBits,
	}
}

// Live returns how many images and shaders the app has on the viewer's side.
func (r *Replayer) Live() (images, shaders int) {
	for _, i := range r.images {
		if !i.screen {
			images++
		}
	}
	return images, len(r.shaders)
}

// version renumbers an app's uniform version. patched marks a block the replay rewrote, which has
// other contents than the app's block of the same version.
func (r *Replayer) version(v uint64, patched bool) uint64 {
	v |= 1<<63 | r.salt
	if patched {
		v |= 1 << 62
	}
	return v
}

// Replay runs batch on g. target is the image the screen's draws go to; it may be nil for a batch
// whose header names no screen. A batch that fails a check returns an error: the operations
// before it have run, and the app's state on this side is then unreliable.
func (r *Replayer) Replay(g graphicsdriver.Graphics, batch []byte, target *Target) (res Result, err error) {
	defer func() {
		// The checks are meant to keep every call the driver gets valid; a panic past them is
		// still the app's input, which must not take the viewer down.
		if e := recover(); e != nil {
			err = fmt.Errorf("remote: replaying the batch: %v", e)
		}
	}()
	h, err := ParseHeader(batch)
	if err != nil {
		return Result{}, err
	}
	if h.Screen != 0 && (target == nil || target.Width != h.Width || target.Height != h.Height) {
		return Result{}, fmt.Errorf("remote: the batch draws to a screen of %dx%d, which the target is not", h.Width, h.Height)
	}
	// Reset vertex/index counts on each Replay: between replays the viewer may overwrite or clear
	// the driver's vertex and index buffers.
	r.vertices, r.indices = 0, 0
	d := &dec{b: batch, p: HeaderSize}
	for d.more() {
		op := d.u8()
		if err := r.op(g, d, op, target, &res); err != nil {
			return res, err
		}
		if d.err != nil {
			return res, d.err
		}
	}
	return res, d.err
}

func (r *Replayer) newID(id uint32) error {
	if id == 0 {
		return errors.New("remote: id 0")
	}
	if _, ok := r.images[id]; ok {
		return fmt.Errorf("remote: id %d is taken", id)
	}
	if _, ok := r.shaders[id]; ok {
		return fmt.Errorf("remote: id %d is taken", id)
	}
	return nil
}

func (r *Replayer) image(id uint32) (*replayedImage, error) {
	i := r.images[id]
	if i == nil {
		return nil, fmt.Errorf("remote: no image %d", id)
	}
	return i, nil
}

// driverImage returns i's image of the driver's: the target for the screen.
func driverImage(i *replayedImage, target *Target) (graphicsdriver.Image, error) {
	if !i.screen {
		return i.img, nil
	}
	if target == nil || target.Image == nil || target.Width != i.width || target.Height != i.height {
		return nil, fmt.Errorf("remote: the screen of %dx%d has no target of its size", i.width, i.height)
	}
	return target.Image, nil
}

func (r *Replayer) op(g graphicsdriver.Graphics, d *dec, op uint8, target *Target, res *Result) error {
	switch op {
	case opSetVertices:
		vertices := d.f32s()
		indices := d.u32s()
		if d.err != nil {
			return d.err
		}
		if len(vertices)%graphics.VertexFloatCount != 0 {
			return fmt.Errorf("remote: %d floats are not whole vertices", len(vertices))
		}
		n := uint32(len(vertices) / graphics.VertexFloatCount)
		for _, i := range indices {
			if i >= n {
				return fmt.Errorf("remote: index %d past %d vertices", i, n)
			}
		}
		r.vertices, r.indices = int(n), len(indices)
		if len(indices) == 0 {
			return nil
		}
		return g.SetVertices(vertices, indices)

	case opNewImage:
		id, w, h, f := d.u32(), d.u32(), d.u32(), graphicsdriver.PixelFormat(d.u8())
		if d.err != nil {
			return d.err
		}
		if err := r.newID(id); err != nil {
			return err
		}
		if max := uint32(g.MaxImageSize()); w == 0 || h == 0 || w > max || h > max {
			return fmt.Errorf("remote: an image of %dx%d", w, h)
		}
		if (f != graphicsdriver.PixelFormatRGBA8 && f != graphicsdriver.PixelFormatA8) || !g.SupportsPixelFormat(f) {
			return fmt.Errorf("remote: pixel format %d", f)
		}
		img, err := g.NewImage(int(w), int(h), f)
		if err != nil {
			return err
		}
		r.images[id] = &replayedImage{img: img, width: int(w), height: int(h), format: f}
		return nil

	case opNewScreenImage:
		id, w, h := d.u32(), d.u32(), d.u32()
		if d.err != nil {
			return d.err
		}
		if err := r.newID(id); err != nil {
			return err
		}
		if w == 0 || h == 0 || w > math.MaxInt32 || h > math.MaxInt32 {
			return fmt.Errorf("remote: a screen of %dx%d", w, h)
		}
		r.images[id] = &replayedImage{width: int(w), height: int(h), screen: true}
		return nil

	case opDisposeImage:
		id := d.u32()
		if d.err != nil {
			return d.err
		}
		i, err := r.image(id)
		if err != nil {
			return err
		}
		if i.img != nil {
			i.img.Dispose()
		}
		delete(r.images, id)
		return nil

	case opNewShader:
		id, textures := d.u32(), d.u32()
		src := d.bytes()
		if d.err != nil {
			return d.err
		}
		if err := r.newID(id); err != nil {
			return err
		}
		if textures > graphics.ShaderSrcImageCount {
			return fmt.Errorf("remote: a shader of %d textures", textures)
		}
		p, err := compile(src, int(textures))
		if err != nil {
			return err
		}
		s, err := g.NewShader(p)
		if err != nil {
			return err
		}
		r.shaders[id] = &replayedShader{shader: s, program: p}
		return nil

	case opDisposeShader:
		id := d.u32()
		if d.err != nil {
			return d.err
		}
		s := r.shaders[id]
		if s == nil {
			return fmt.Errorf("remote: no shader %d", id)
		}
		s.shader.Dispose()
		delete(r.shaders, id)
		return nil

	case opWritePixels:
		id := d.u32()
		n := d.count(20)
		if d.err != nil {
			return d.err
		}
		i, err := r.image(id)
		if err != nil {
			return err
		}
		if i.screen {
			return errors.New("remote: pixels written to the screen")
		}
		args := make([]graphicsdriver.PixelsArgs, 0, n)
		for range n {
			region := d.rect()
			pix := d.bytes()
			if d.err != nil {
				return d.err
			}
			if err := checkPixels(i, region, len(pix)); err != nil {
				return err
			}
			if !region.Empty() {
				args = append(args, graphicsdriver.PixelsArgs{Pixels: pix, Region: region})
			}
		}
		if len(args) == 0 {
			return nil
		}
		return i.img.WritePixels(args)

	case opDrawTriangles:
		return r.draw(g, d, target, res)

	case opPresent:
		id := d.u32()
		n := d.u32()
		if d.err != nil {
			return d.err
		}
		i, err := r.image(id)
		if err != nil {
			return err
		}
		if !i.screen {
			return fmt.Errorf("remote: image %d presented, which is not a screen", id)
		}
		res.Presented, res.Damage = true, nil
		if n == allDamaged {
			return nil
		}
		if uint64(n)*16 > uint64(len(d.b)-d.p) {
			return errShort
		}
		bounds := image.Rect(0, 0, i.width, i.height)
		res.Damage = make([]image.Rectangle, 0, n)
		for range n {
			if rect := d.rect().Intersect(bounds); !rect.Empty() {
				res.Damage = append(res.Damage, rect)
			}
		}
		return d.err
	}
	return fmt.Errorf("remote: unknown operation %d at %d", op, d.p-1)
}

func checkPixels(i *replayedImage, region image.Rectangle, n int) error {
	if region.Min.X > region.Max.X || region.Min.Y > region.Max.Y {
		return fmt.Errorf("remote: inverted pixel region %v", region)
	}
	bounds := i.bounds()
	if region.Empty() {
		if n != 0 {
			return fmt.Errorf("remote: %d bytes of pixels for empty region %v, not 0", n, region)
		}
		return nil
	}
	if !region.In(bounds) {
		return fmt.Errorf("remote: pixels at %v, out of an image of %v", region, bounds)
	}
	want := uint64(i.format.BytesPerPixel()) * uint64(region.Dx()) * uint64(region.Dy())
	if n < 0 || uint64(n) != want {
		return fmt.Errorf("remote: %d bytes of pixels for %v, not %d", n, region, want)
	}
	return nil
}

func (r *Replayer) draw(g graphicsdriver.Graphics, d *dec, target *Target, res *Result) error {
	dstID := d.u32()
	var srcIDs [graphics.ShaderSrcImageCount]uint32
	for k := range srcIDs {
		srcIDs[k] = d.u32()
	}
	shaderID := d.u32()
	n := d.count(20)
	if d.err != nil {
		return d.err
	}
	dst, err := r.image(dstID)
	if err != nil {
		return err
	}
	dstImg, err := driverImage(dst, target)
	if err != nil {
		return err
	}
	var srcs [graphics.ShaderSrcImageCount]graphicsdriver.Image
	for k, id := range srcIDs {
		if id == 0 {
			continue
		}
		if id == dstID {
			return errors.New("remote: an image drawn to itself")
		}
		src, err := r.image(id)
		if err != nil {
			return err
		}
		if src.screen {
			return errors.New("remote: the screen used as a source")
		}
		srcs[k] = src.img
	}
	sh := r.shaders[shaderID]
	if sh == nil {
		return fmt.Errorf("remote: no shader %d", shaderID)
	}

	bounds := dst.bounds()
	regions := make([]graphicsdriver.DstRegion, n)
	var indices uint64
	for k := range regions {
		reg := d.rect()
		c := d.u32()
		if d.err != nil {
			return d.err
		}
		if reg.Min.X < bounds.Min.X || reg.Min.Y < bounds.Min.Y || reg.Max.X > bounds.Max.X || reg.Max.Y > bounds.Max.Y || reg.Min.X > reg.Max.X || reg.Min.Y > reg.Max.Y {
			return fmt.Errorf("remote: a draw to %v, out of an image of %v", reg, bounds)
		}
		indices += uint64(c)
		if indices > uint64(r.indices) {
			return fmt.Errorf("remote: a draw of more indices than the %d set", r.indices)
		}
		regions[k] = graphicsdriver.DstRegion{
			Region:     reg,
			IndexCount: int(c),
		}
	}
	offset := uint64(d.u32())
	var b [6]uint8
	for k := range b {
		b[k] = d.u8()
	}
	internal, user := d.u64(), d.u64()
	uniforms := d.u32s()
	if d.err != nil {
		return d.err
	}
	if offset > uint64(r.indices)-indices {
		return fmt.Errorf("remote: a draw from index %d past the %d set", offset, r.indices)
	}
	for k := range 4 {
		if graphicsdriver.BlendFactor(b[k]) > graphicsdriver.BlendFactorSourceAlphaSaturated {
			return fmt.Errorf("remote: blend factor %d", b[k])
		}
	}
	for k := 4; k < 6; k++ {
		if graphicsdriver.BlendOperation(b[k]) > graphicsdriver.BlendOperationMax {
			return fmt.Errorf("remote: blend operation %d", b[k])
		}
	}
	blend := graphicsdriver.Blend{
		BlendFactorSourceRGB:        graphicsdriver.BlendFactor(b[0]),
		BlendFactorSourceAlpha:      graphicsdriver.BlendFactor(b[1]),
		BlendFactorDestinationRGB:   graphicsdriver.BlendFactor(b[2]),
		BlendFactorDestinationAlpha: graphicsdriver.BlendFactor(b[3]),
		BlendOperationRGB:           graphicsdriver.BlendOperation(b[4]),
		BlendOperationAlpha:         graphicsdriver.BlendOperation(b[5]),
	}
	if internal == 0 || user == 0 || internal >= 1<<versionBits || user >= 1<<versionBits {
		return fmt.Errorf("remote: uniform versions %d, %d", internal, user)
	}
	if want := graphics.PreservedUniformDwordCount + sh.program.UniformDwordCount; len(uniforms) != want {
		return fmt.Errorf("remote: %d dwords of uniforms for a shader of %d", len(uniforms), want)
	}
	// The uniforms are used in place unless they are rewritten for the screen.
	if dst.screen {
		uniforms = retarget(uniforms, target)
	}
	versions := graphicsdriver.UniformVersions{
		Internal: r.version(internal, dst.screen),
		User:     r.version(user, false),
	}
	res.Draws++
	return g.DrawTriangles(dstImg, srcs, sh.shader, regions, int(offset), blend, uniforms, versions)
}

// retarget returns a copy of uniforms written for a screen of the target's size rewritten for the
// target, which draws at its internal size: the destination's texture size and the scale of the
// projection, whose terms graphicscommand writes as 2/width and ±2/height (see
// prependPreservedUniforms there). The rest, in pixels of the destination, holds as it is.
func retarget(uniforms []uint32, target *Target) []uint32 {
	u := append([]uint32(nil), uniforms...)
	w, h := float32(target.InternalWidth), float32(target.InternalHeight)
	u[graphics.DstTextureSizeUniformDwordIndex] = math.Float32bits(w)
	u[graphics.DstTextureSizeUniformDwordIndex+1] = math.Float32bits(h)
	p := u[graphics.ProjectionMatrixUniformDwordIndex:]
	p[0] = math.Float32bits(2 / w)
	p[5] = math.Float32bits(2/h) | p[5]&(1<<31)
	return u
}

// ReadPixels reads regions of the app's image id into dsts. It is called after the batch that
// came with the request was replayed.
func (r *Replayer) ReadPixels(id uint32, regions []image.Rectangle, dsts [][]byte, target *Target) error {
	i, err := r.image(id)
	if err != nil {
		return err
	}
	img, err := driverImage(i, target)
	if err != nil {
		return err
	}
	format := i.format
	if i.screen {
		format = graphicsdriver.PixelFormatRGBA8
	}
	if len(regions) != len(dsts) {
		return errors.New("remote: a read of pixels with as many regions as buffers")
	}
	args := make([]graphicsdriver.PixelsArgs, 0, len(regions))
	for k, region := range regions {
		if err := checkPixels(&replayedImage{width: i.width, height: i.height, format: format, screen: i.screen}, region, len(dsts[k])); err != nil {
			return err
		}
		if !region.Empty() {
			args = append(args, graphicsdriver.PixelsArgs{Pixels: dsts[k], Region: region})
		}
	}
	if len(args) == 0 {
		return nil
	}
	return img.ReadPixels(args)
}

// Dispose frees every image and shader the app made. The screen is the viewer's and stays.
func (r *Replayer) Dispose() {
	for _, i := range r.images {
		if i.img != nil {
			i.img.Dispose()
		}
	}
	for _, s := range r.shaders {
		s.shader.Dispose()
	}
	clear(r.images)
	clear(r.shaders)
	r.vertices, r.indices = 0, 0
}

// programs caches the programs compiled from the sources apps sent, so that an app's restart
// skips the parse and the validation of its shaders. The compiled programs are immutable.
var (
	programsM sync.Mutex
	programs  = map[[32]byte]*shader.Program{}
)

// maxPrograms bounds the cache, which a viewer running app after app would otherwise grow.
const maxPrograms = 512

func compile(src []byte, textures int) (p *shader.Program, err error) {
	h := sha256.New()
	var n [4]byte
	binary.LittleEndian.PutUint32(n[:], uint32(textures))
	h.Write(n[:])
	h.Write(src)
	var k [32]byte
	h.Sum(k[:0])

	programsM.Lock()
	defer programsM.Unlock()
	if p, ok := programs[k]; ok {
		return p, nil
	}
	defer func() {
		if e := recover(); e != nil {
			p, err = nil, fmt.Errorf("remote: compiling a shader: %v", e)
		}
	}()
	p, err = shader.Compile(src, textures)
	if err != nil {
		return nil, err
	}
	if len(programs) >= maxPrograms {
		clear(programs)
	}
	programs[k] = p
	return p, nil
}
