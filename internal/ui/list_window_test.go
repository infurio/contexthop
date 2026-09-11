package ui

import "testing"

func TestVisibleListRange(t *testing.T) {
	for _, tc := range []struct{ count, cursor, capacity, start, end int }{
		{0, 0, 5, 0, 0}, {20, 10, 0, 0, 0}, {20, 10, -1, 0, 0},
		{20, 0, 5, 0, 5}, {20, 4, 5, 0, 5}, {20, 5, 5, 1, 6},
		{20, 19, 5, 15, 20}, {20, 19, 1, 19, 20}, {3, 2, 10, 0, 3},
	} {
		start, end := visibleListRange(tc.count, tc.cursor, tc.capacity)
		if start != tc.start || end != tc.end {
			t.Fatalf("%+v: got [%d,%d)", tc, start, end)
		}
	}
}

func TestVisibleListRangeKeepsFocusThroughResizes(t *testing.T) {
	for count := 1; count <= 30; count++ {
		for cursor := 0; cursor < count; cursor++ {
			for _, capacity := range []int{1, 2, 5, 15, 40, 5, 1} {
				start, end := visibleListRange(count, cursor, capacity)
				if start < 0 || end > count || start > cursor || end <= cursor || end-start > capacity {
					t.Fatalf("count=%d cursor=%d capacity=%d: [%d,%d)", count, cursor, capacity, start, end)
				}
			}
		}
	}
}
