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

package graphicscommand

import (
	"github.com/ironpark/ggfx/internal/graphicsdriver"
)

// externalCommand runs a function of the caller's with the driver. A remote app's draws are
// replayed so: they are the app's driver calls, made as they came, which no command of this queue
// can express.
type externalCommand struct {
	f func(graphicsdriver.Graphics) error
}

func (c *externalCommand) String() string { return "external" }

func (c *externalCommand) Exec(commandQueue *commandQueue, graphicsDriver graphicsdriver.Graphics, indexOffset int) error {
	return c.f(graphicsDriver)
}

// NeedsSync reports true: the caller waits for f, whose input it owns only until f returns.
func (c *externalCommand) NeedsSync() bool { return true }

// RunExternal runs f on the render thread with the driver, after every command queued so far, and
// waits for it. f runs inside the driver's Begin and End, as the last command of an intermediate
// flush: it may set vertices of its own, as no command of the queue runs after it in that batch.
func RunExternal(graphicsDriver graphicsdriver.Graphics, f func(graphicsdriver.Graphics) error) error {
	theCommandQueueManager.enqueueCommand(&externalCommand{f: f})
	return theCommandQueueManager.flush(graphicsDriver, graphicsdriver.FlushModeIntermediate)
}

// DriverImage returns the driver's image of i. It is valid on the render thread, once the command
// that makes it has run: in the f of a RunExternal enqueued after it, say.
func (i *Image) DriverImage() graphicsdriver.Image {
	return i.image
}
