package main

import "time"

// The history measures elapsed listening time, not seekable file position.
// Record loops from the active stream's END sync, including multiple wraps
// between two UI samples. Pause/seek must not contribute a jump on the X axis.
type historyClock struct {
	last    time.Time
	elapsed float64
	pending int
}

func (c *historyClock) step(now time.Time, active bool) float64 {
	if !c.last.IsZero() {
		c.elapsed += now.Sub(c.last).Seconds()
	}
	if active {
		c.last = now
	} else {
		c.last = time.Time{}
	}
	return c.elapsed
}
func (c *historyClock) markers(dst []float64, duration, pos float64) []float64 {
	n := c.pending
	c.pending = 0
	if n == 0 || duration <= 0 {
		return dst
	}
	if n > 4096 {
		n = 4096
	}
	// Timestamp queued ends against the current pass, not message-delivery time.
	last := c.elapsed - max(0, min(pos, duration))
	for i := n - 1; i >= 0; i-- {
		at := last - float64(i)*duration
		if at >= 0 && (len(dst) == 0 || at > dst[len(dst)-1]) {
			dst = append(dst, at)
		}
	}
	if len(dst) > 4096 {
		copy(dst, dst[len(dst)-4096:])
		dst = dst[:4096]
	}
	return dst
}
