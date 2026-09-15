package main

// speculativeAdditionalForbiddenPrimitive closes side-effect gaps that were
// added after the v27 speculative baseline. These operations mutate global,
// remote, or physical storage topology and therefore must execute only on the
// canonical path.
func speculativeAdditionalForbiddenPrimitive(code string) bool {
	switch code {
	case "mesh_shared_reconcile", "mesh_route_execution", "physical_execution_concurrency_set",
		"memory_new", "memory_copy":
		// memory_new/memory_copy eventually choose/create a bounded physical
		// shard. That placement is a real storage-topology side effect and cannot
		// occur inside a discardable speculative snapshot.
		return true
	default:
		return false
	}
}
