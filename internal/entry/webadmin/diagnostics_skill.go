package webadmin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"robot/internal/capability/catalog"
	"robot/internal/shared"
)

var partySkillErrorLogPatterns = []string{
	"PARTY_DUNGEON_SKILL_PROFILE_ERROR",
	"PARTY_DUNGEON_SKILL_CAST_ERROR",
	"PARTY_DUNGEON_SKILL_RECOVER_ERROR",
}

func (b *diagnosticsBuilder) addSkillSection() {
	configDir := b.cfg.ConfigDir
	checks := skillDiagnosticsChecks(configDir)
	checks = append(checks, recentLogPatternCheck("recent party skill errors", filepath.Join(configDir, "log_robot"), partySkillErrorLogPatterns))
	b.addSection("Skill", checks...)
}

func skillDiagnosticsChecks(configDir string) []diagnosticsCheck {
	whitelistPath := filepath.Join(configDir, "party_skill_catalog.json")
	pvfPath := filepath.Join(configDir, "pvf_skill_state_catalog.json")

	whitelist, whitelistErr := catalog.ReadPartySkillCatalog(whitelistPath)
	whitelistCheck := partySkillWhitelistCheck(whitelistPath, whitelist, whitelistErr)
	pvfStates, pvfCheck, pvfErr := pvfSkillCatalogCheck(pvfPath)
	checks := []diagnosticsCheck{whitelistCheck, pvfCheck}
	if whitelistErr != nil || pvfErr != nil {
		checks = append(checks, diagnosticsCheck{
			Name: "effective party skill candidates", Status: diagError,
			Message: "candidate intersection cannot be calculated until both catalogs are valid",
		})
		return checks
	}
	checks = append(checks, effectivePartySkillCheck(whitelist, pvfStates))
	return checks
}

func partySkillWhitelistCheck(path string, report catalog.PartySkillCatalogReport, err error) diagnosticsCheck {
	if err != nil {
		return diagnosticsCheck{Name: filepath.Base(path), Status: diagError, Message: err.Error(), Expected: path}
	}
	byJob := make(map[int]int)
	for _, entry := range report.Entries {
		byJob[entry.Job]++
	}
	status := diagOK
	message := "party skill whitelist is valid"
	switch {
	case len(report.Entries) == 0:
		status = diagWarn
		message = "party skill whitelist has no valid entries"
	case len(report.Issues) > 0 || report.OverLevelCount > 0 || report.ConfiguredMaxSkillLevel > report.EffectiveMaxSkillLevel:
		status = diagWarn
		message = "party skill whitelist contains skipped entries"
	}
	return diagnosticsCheck{
		Name: filepath.Base(path), Status: status, Message: message,
		Observed: map[string]interface{}{
			"path": path, "source_count": report.SourceCount, "valid_count": len(report.Entries), "by_job": byJob,
			"invalid_count": len(report.Issues), "disabled_count": report.DisabledCount, "over_level_count": report.OverLevelCount,
			"configured_max_level": report.ConfiguredMaxSkillLevel, "effective_max_level": report.EffectiveMaxSkillLevel,
		},
	}
}

func pvfSkillCatalogCheck(path string) ([]shared.SkillState, diagnosticsCheck, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, diagnosticsCheck{Name: filepath.Base(path), Status: diagError, Message: err.Error(), Expected: path}, err
	}
	var entries []shared.SkillState
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, diagnosticsCheck{Name: filepath.Base(path), Status: diagError, Message: err.Error(), Expected: path}, err
	}
	byJob := make(map[int]int)
	for _, entry := range entries {
		byJob[entry.Job]++
	}
	status := diagOK
	message := "PVF skill catalog is valid"
	if len(entries) == 0 {
		status = diagWarn
		message = "PVF skill catalog has no entries"
	}
	return entries, diagnosticsCheck{
		Name: filepath.Base(path), Status: status, Message: message,
		Observed: map[string]interface{}{"path": path, "count": len(entries), "by_job": byJob},
	}, nil
}

func effectivePartySkillCheck(whitelist catalog.PartySkillCatalogReport, pvfStates []shared.SkillState) diagnosticsCheck {
	jobSet := make(map[int]struct{})
	for _, entry := range whitelist.Entries {
		jobSet[entry.Job] = struct{}{}
	}
	jobs := make([]int, 0, len(jobSet))
	for job := range jobSet {
		jobs = append(jobs, job)
	}
	sort.Ints(jobs)

	byJob := make(map[int]int, len(jobs))
	total := 0
	stats := shared.PartySkillMatchStats{}
	for _, job := range jobs {
		matches, jobStats := shared.MatchPartySkillStates(job, whitelist.Entries, pvfStates)
		byJob[job] = len(matches)
		total += len(matches)
		stats.PVFMatched += jobStats.PVFMatched
		stats.SkippedMissingPVF += jobStats.SkippedMissingPVF
		stats.SkippedPathMismatch += jobStats.SkippedPathMismatch
	}

	status := diagOK
	message := fmt.Sprintf("%d effective party skill candidates", total)
	switch {
	case total == 0:
		status = diagError
		message = "party skill whitelist and PVF export have no effective candidates"
	case len(whitelist.Issues) > 0 || whitelist.OverLevelCount > 0 || stats.SkippedMissingPVF > 0 || stats.SkippedPathMismatch > 0:
		status = diagWarn
		message = fmt.Sprintf("%d effective party skill candidates with skipped entries", total)
	}
	return diagnosticsCheck{
		Name: "effective party skill candidates", Status: status, Message: message,
		Observed: map[string]interface{}{
			"total": total, "by_job": byJob, "whitelist_valid": len(whitelist.Entries), "whitelist_invalid": len(whitelist.Issues),
			"pvf_total": len(pvfStates), "missing_pvf": stats.SkippedMissingPVF, "path_mismatch": stats.SkippedPathMismatch,
		},
	}
}
