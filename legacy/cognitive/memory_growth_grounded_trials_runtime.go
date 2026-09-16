package main

import (
	"errors"
	"sort"
	"strings"
)

func RunGroundedTrialAuthoringCycle(e *Engine) (*GroundedTrialAuthoringResult, error) {
	result := &GroundedTrialAuthoringResult{}
	root := memoryGrowthRoot(e)
	if root == nil {
		return result, errors.New("engine unavailable")
	}
	experiences, formation, validation, err := LoadMemoryGrowthState(root)
	if err != nil {
		return result, err
	}
	structures := validation.Snapshot()
	sort.Slice(structures, func(i, j int) bool { return structures[i].ID < structures[j].ID })
	for _, structure := range structures {
		if structure == nil || strings.TrimSpace(structure.Action[groundedTrialSeriesActionKey]) == "" {
			continue
		}
		instance, updated, created, err := authorizeNextGroundedTrialInstance(root, experiences, validation, structure)
		if err != nil {
			return result, err
		}
		if !created || instance == nil || updated == nil {
			continue
		}
		if err := PersistMemoryGrowthState(root, experiences, formation, validation); err != nil {
			return result, err
		}
		if err := root.persistAll(); err != nil {
			return result, err
		}
		result.StructureID = updated.ID
		result.SeriesID = instance.SeriesID
		result.ActionInstanceID = instance.ID
		result.ActionID = instance.ActionID
		result.ParentExperienceID = instance.ParentExperienceID
		result.Generation = instance.Generation
		result.Persisted = true
		return result, nil
	}
	result.Skipped = true
	return result, nil
}
