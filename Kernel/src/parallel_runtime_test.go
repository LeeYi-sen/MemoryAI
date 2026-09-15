package main

import "testing"

func TestParallelDotHasNoFixedLaneCount(t *testing.T) {
	for _, tc := range []struct {
		name    string
		vectors [][]float32
		weights []float32
		want    []float32
	}{
		{
			name:    "three-lanes",
			vectors: [][]float32{{1, 2, 3}, {3, 2, 1}},
			weights: []float32{1, 2, 3},
			want:    []float32{14, 10},
		},
		{
			name:    "eleven-lanes",
			vectors: [][]float32{{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1}},
			weights: []float32{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1},
			want:    []float32{11},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, backend, err := globalParallelRuntime.Dot(tc.vectors, tc.weights)
			if err != nil {
				t.Fatal(err)
			}
			if backend != "cpu-dot" || len(got) != len(tc.want) {
				t.Fatalf("unexpected physical dot result: backend=%q got=%v", backend, got)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("row %d mismatch: got=%v want=%v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestParallelDotRejectsLaneMismatch(t *testing.T) {
	if _, _, err := globalParallelRuntime.Dot([][]float32{{1, 2}}, []float32{1, 2, 3}); err == nil {
		t.Fatal("dimension-agnostic physical dot accepted mismatched row length")
	}
	if _, _, err := globalParallelRuntime.Dot(nil, nil); err == nil {
		t.Fatal("physical dot accepted zero-lane vector definition")
	}
}
