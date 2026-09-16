//go:build !linux || !cgo
// +build !linux !cgo

package main

type unavailablePhysicalGPU struct{}

func newPhysicalGPUBackend() physicalGPUBackend { return unavailablePhysicalGPU{} }
func (unavailablePhysicalGPU) Name() string     { return "unavailable" }
func (unavailablePhysicalGPU) Available() bool  { return false }
func (unavailablePhysicalGPU) Dot(_ [][]float32, _ []float32) ([]float32, error) {
	return nil, errPhysicalGPUUnavailable
}
func (unavailablePhysicalGPU) Info() map[string]any {
	return map[string]any{"available": false, "backend": "none"}
}
