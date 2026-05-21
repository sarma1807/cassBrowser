package main

import (
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
)

// DebouncedLayout wraps content and delays layout during resize operations
type DebouncedLayout struct {
	content      fyne.CanvasObject
	lastSize     fyne.Size
	debounceTime time.Duration
	timer        *time.Timer
	mu           sync.Mutex
	isResizing   bool
}

// NewDebouncedLayout creates a layout that debounces resize operations
func NewDebouncedLayout(content fyne.CanvasObject, debounceMs int) *DebouncedLayout {
	return &DebouncedLayout{
		content:      content,
		debounceTime: time.Duration(debounceMs) * time.Millisecond,
	}
}

// Layout positions the content - debounced during resize
func (d *DebouncedLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Check if size actually changed
	if d.lastSize.Width == size.Width && d.lastSize.Height == size.Height {
		// No change, perform normal layout
		if len(objects) > 0 {
			objects[0].Resize(size)
			objects[0].Move(fyne.NewPos(0, 0))
		}
		return
	}

	// Size changed - we're resizing
	d.lastSize = size

	// Cancel existing timer
	if d.timer != nil {
		d.timer.Stop()
	}

	// If not already marked as resizing, mark it now
	if !d.isResizing {
		d.isResizing = true
	}

	// Start new debounce timer
	d.timer = time.AfterFunc(d.debounceTime, func() {
		d.mu.Lock()
		d.isResizing = false
		d.mu.Unlock()

		fyne.Do(func() {
			if len(objects) > 0 {
				objects[0].Resize(size)
				objects[0].Move(fyne.NewPos(0, 0))
				objects[0].Refresh()
			}
		})
	})

	// During resize, still update position but skip heavy refresh
	if len(objects) > 0 {
		objects[0].Resize(size)
		objects[0].Move(fyne.NewPos(0, 0))
	}
}

// MinSize returns the minimum size required
func (d *DebouncedLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) > 0 {
		return objects[0].MinSize()
	}
	return fyne.NewSize(0, 0)
}

// DebouncedContainer is a container that uses debounced layout
type DebouncedContainer struct {
	fyne.Container
	layout *DebouncedLayout
}

// NewDebouncedContainer creates a container with debounced resize
func NewDebouncedContainer(content fyne.CanvasObject, debounceMs int) *fyne.Container {
	layout := NewDebouncedLayout(content, debounceMs)
	return container.New(layout, content)
}

// NewDebouncedHSplit creates a horizontal split with debounced panel resize
func NewDebouncedHSplit(leading, trailing fyne.CanvasObject, debounceMs int) *container.Split {
	debouncedLeading := NewDebouncedContainer(leading, debounceMs)
	debouncedTrailing := NewDebouncedContainer(trailing, debounceMs)
	return container.NewHSplit(debouncedLeading, debouncedTrailing)
}
