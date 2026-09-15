package main

// speculativeAdditionalForbiddenPrimitive closes side-effect gaps that were
// added after the v27 speculative baseline. These operations mutate global or
// remote physical state and therefore must execute only on the canonical path.
func speculativeAdditionalForbiddenPrimitive(code string) bool {
	switch code {
	case "mesh_shared_reconcile", "mesh_route_cognition", "cognition_concurrency_set":
		return true
	default:
		return false
	}
}
