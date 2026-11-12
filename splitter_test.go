package main

import "testing"

func TestSplitterHandleRectAlignsWithAxis(t *testing.T) {
	tests := []struct {
		name     string
		splitter RECT
		vertical bool
	}{
		{"vertical", RECT{100, 20, 107, 220}, true},
		{"vertical short", RECT{100, 20, 107, 55}, true},
		{"horizontal", RECT{30, 80, 230, 87}, false},
		{"horizontal short", RECT{30, 80, 65, 87}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handle := splitterHandleRect(tt.splitter, tt.vertical)
			if tt.vertical {
				wantAxis := tt.splitter.Left + rectW(tt.splitter)/2
				gotAxis := handle.Left + rectW(handle)/2
				if gotAxis != wantAxis {
					t.Fatalf("vertical handle axis = %d, want %d", gotAxis, wantAxis)
				}
				wantLength := splitterHandleLength
				if rectH(tt.splitter) < wantLength {
					wantLength = rectH(tt.splitter)
				}
				if rectH(handle) != wantLength {
					t.Fatalf("vertical handle length = %d", rectH(handle))
				}
			} else {
				wantAxis := tt.splitter.Top + rectH(tt.splitter)/2
				gotAxis := handle.Top + rectH(handle)/2
				if gotAxis != wantAxis {
					t.Fatalf("horizontal handle axis = %d, want %d", gotAxis, wantAxis)
				}
				wantLength := splitterHandleLength
				if rectW(tt.splitter) < wantLength {
					wantLength = rectW(tt.splitter)
				}
				if rectW(handle) != wantLength {
					t.Fatalf("horizontal handle length = %d", rectW(handle))
				}
			}
		})
	}
}

func TestClearMouseHoverResetsTrackingAndQueuesRepaint(t *testing.T) {
	a := &App{
		hover:              POINT{42, 84},
		mouseLeaveTracking: true,
	}
	a.clearMouseHover()

	if a.mouseLeaveTracking {
		t.Fatal("mouse leave tracking remained active")
	}
	if a.hover != hoverOutsidePoint() {
		t.Fatalf("hover = %+v, want outside point", a.hover)
	}
	if !a.hoverDirty {
		t.Fatal("hover repaint was not queued")
	}
}
