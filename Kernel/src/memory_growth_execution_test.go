package main

import "testing"

func TestBindExecutableProgramRequiresValidatedStructure(t *testing.T) {
	structure := &MemoryStructure{ID: "structure-candidate", State: memoryStructureCandidateState}
	program := []Op{{Code: "set", A: "result", B: "ok"}, {Code: "halt"}}
	if err := BindExecutableProgram(structure, program); err == nil {
		t.Fatal("candidate structure must not become executable")
	}
}

func TestExecutableMemoryCarriesValidatedProgram(t *testing.T) {
	structure := &MemoryStructure{
		ID:          "memory-structure-executable",
		PatternHash: "pattern-hash",
		State:       memoryStructureValidatedState,
		Context:     map[string]string{"state": "ready"},
	}
	program := []Op{{Code: "set", A: "result", B: "ok"}, {Code: "halt"}}
	if err := BindExecutableProgram(structure, program); err != nil {
		t.Fatalf("BindExecutableProgram() error = %v", err)
	}
	memory, err := ExecutableMemory(structure)
	if err != nil {
		t.Fatalf("ExecutableMemory() error = %v", err)
	}
	if memory.ID != structure.ID || len(memory.Program) != len(program) {
		t.Fatalf("executable Memory lost identity/program: id=%q program=%d", memory.ID, len(memory.Program))
	}
	if got := memory.State["memory_structure_id"]; got != structure.ID {
		t.Fatalf("structure id state = %v", got)
	}
}

func TestExecutableProgramIsCopied(t *testing.T) {
	structure := &MemoryStructure{
		ID:    "memory-structure-copy",
		State: memoryStructureValidatedState,
	}
	program := []Op{{Code: "set", A: "result", B: "original"}, {Code: "halt"}}
	if err := BindExecutableProgram(structure, program); err != nil {
		t.Fatalf("BindExecutableProgram() error = %v", err)
	}
	program[0].B = "mutated-source"
	if structure.Program[0].B != "original" {
		t.Fatal("structure program aliases caller memory")
	}
}
