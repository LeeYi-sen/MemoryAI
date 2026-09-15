package main

import "testing"

func TestFabricRootHintInvalidatesWhenCoreStoreCloses(t *testing.T) {
	e, owner := newActivationFabricTestEngine(t)
	if got := fabricRootFor(owner); got != e {
		t.Fatalf("mounted shard did not resolve to core Fabric root: got=%p want=%p", got, e)
	}

	e.closeStore()
	if got := fabricRootFor(owner); got != owner {
		t.Fatalf("closed Fabric root remained authoritative: got=%p owner=%p", got, owner)
	}
}
