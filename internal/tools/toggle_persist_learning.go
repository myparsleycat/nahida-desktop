package tools

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	togglePersistProfileFile    = ".togglepersistprofile.json"
	togglePersistProfileVersion = 1
	observationWindowMs         = 600_000
	observationLimit            = 128
	initialQuietMs              = 3_000
	maximumQuietMs              = 10_000
)

type TogglePersistLearnedVariable struct {
	Name             string  `json:"name"`
	MedianIntervalMs float64 `json:"medianIntervalMs"`
	LearnedAt        string  `json:"learnedAt"`
}

type TogglePersistProfileFile struct {
	Fingerprint string                                  `json:"fingerprint"`
	Variables   map[string]TogglePersistLearnedVariable `json:"variables"`
}

type TogglePersistProfile struct {
	Version int                                 `json:"version"`
	Files   map[string]TogglePersistProfileFile `json:"files"`
}

type persistObservation struct {
	at           int64
	revision     int
	value        string
	numericValue *float64
}

type persistVariableState struct {
	name                       string
	observations               []persistObservation
	pendingValue               *string
	pendingDueAt               *int64
	status                     string
	learnedProfile             *TogglePersistLearnedVariable
	learnedInSession           bool
	cohortSuppressed           bool
	sparseCohortSuppressed     bool
	suppressedObservationCount *int
}

type persistFileState struct {
	order     []string
	variables map[string]*persistVariableState
}

type TogglePersistObservationResult struct {
	NewlySuppressed []string
	NewlyLearned    []TogglePersistLearnedVariable
	NextDueAt       *int64
}

type TogglePersistReadyResult struct {
	Updates   [][2]string
	NextDueAt *int64
}

type TogglePersistLearner struct {
	files map[string]*persistFileState
}

func newTogglePersistLearner() *TogglePersistLearner {
	return &TogglePersistLearner{files: map[string]*persistFileState{}}
}

func (l *TogglePersistLearner) RegisterLearnedVariables(targetINIPath string, variables map[string]TogglePersistLearnedVariable) {
	file := l.requireFile(targetINIPath)
	for varName, learned := range variables {
		varKey := strings.ToLower(varName)
		if state, ok := file.variables[varKey]; ok {
			copied := learned
			state.learnedProfile = &copied
			continue
		}
		copied := learned
		file.order = append(file.order, varKey)
		file.variables[varKey] = &persistVariableState{
			name: learned.Name, observations: nil, status: "observing",
			learnedProfile: &copied,
		}
	}
}

func (l *TogglePersistLearner) Observe(targetINIPath, varName, value string, revision int, at int64) TogglePersistObservationResult {
	file := l.requireFile(targetINIPath)
	varKey := strings.ToLower(varName)
	state, ok := file.variables[varKey]
	if !ok {
		state = &persistVariableState{name: varName, status: "observing"}
		file.order = append(file.order, varKey)
		file.variables[varKey] = state
	}
	previousMedianInterval := medianInterval(state.observations)
	var lastObservation *persistObservation
	if len(state.observations) > 0 {
		last := state.observations[len(state.observations)-1]
		lastObservation = &last
	}
	if lastObservation != nil && at-lastObservation.at > observationWindowMs {
		state.observations = nil
		state.status = "observing"
		state.cohortSuppressed = false
		state.sparseCohortSuppressed = false
		state.suppressedObservationCount = nil
	}
	baseQuiet := float64(initialQuietMs)
	if previousMedianInterval != nil {
		baseQuiet = *previousMedianInterval
	}
	cooldownMs := math.Min(120_000, math.Max(30_000, baseQuiet*5))
	if state.status == "suppressed" && lastObservation != nil && float64(at-lastObservation.at) > cooldownMs {
		state.observations = nil
		state.status = "observing"
		state.cohortSuppressed = false
		state.sparseCohortSuppressed = false
		state.suppressedObservationCount = nil
	}
	state.name = varName
	state.observations = append(state.observations, persistObservation{
		at: at, revision: revision, value: value, numericValue: parseFiniteNumber(value),
	})
	cutoff := at - observationWindowMs
	filtered := state.observations[:0]
	for _, observation := range state.observations {
		if observation.at >= cutoff {
			filtered = append(filtered, observation)
		}
	}
	if len(filtered) > observationLimit {
		filtered = filtered[len(filtered)-observationLimit:]
	}
	state.observations = append([]persistObservation{}, filtered...)
	pending := value
	due := at + quietWindow(state)
	state.pendingValue = &pending
	state.pendingDueAt = &due
	newlySuppressed := l.evaluateSuppression(file)
	newlyLearned := l.evaluateLearning(file, at)
	if state.status == "suppressed" {
		state.pendingValue = nil
		state.pendingDueAt = nil
	}
	return TogglePersistObservationResult{NewlySuppressed: newlySuppressed, NewlyLearned: newlyLearned, NextDueAt: nextDueAt(file)}
}

func (l *TogglePersistLearner) TakeReady(targetINIPath string, at int64) TogglePersistReadyResult {
	file := l.files[fileKey(targetINIPath)]
	if file == nil {
		return TogglePersistReadyResult{}
	}
	var updates [][2]string
	for _, varName := range file.order {
		state := file.variables[varName]
		if state.status == "suppressed" || state.pendingValue == nil || state.pendingDueAt == nil || *state.pendingDueAt > at {
			continue
		}
		updates = append(updates, [2]string{varName, *state.pendingValue})
		state.pendingValue = nil
		state.pendingDueAt = nil
		if state.status == "suspected" {
			state.status = "observing"
			state.cohortSuppressed = false
			state.sparseCohortSuppressed = false
			state.suppressedObservationCount = nil
		}
	}
	return TogglePersistReadyResult{Updates: updates, NextDueAt: nextDueAt(file)}
}

func (l *TogglePersistLearner) GetNextDueAt(targetINIPath string) *int64 {
	file := l.files[fileKey(targetINIPath)]
	if file == nil {
		return nil
	}
	return nextDueAt(file)
}

func (l *TogglePersistLearner) Clear() {
	l.files = map[string]*persistFileState{}
}

func (l *TogglePersistLearner) requireFile(targetINIPath string) *persistFileState {
	key := fileKey(targetINIPath)
	if existing := l.files[key]; existing != nil {
		return existing
	}
	created := &persistFileState{variables: map[string]*persistVariableState{}}
	l.files[key] = created
	return created
}

func (l *TogglePersistLearner) evaluateSuppression(file *persistFileState) []string {
	var newlySuppressed []string
	for _, varKey := range file.order {
		state := file.variables[varKey]
		if state.status == "suppressed" {
			continue
		}
		evidence := runtimeEvidence(file, state)
		observations := state.observations
		span := observationSpan(observations)
		distinct := map[string]struct{}{}
		for _, observation := range observations {
			distinct[observation.value] = struct{}{}
		}
		observedMedianInterval := medianInterval(observations)
		learnedThreshold := state.learnedProfile != nil &&
			len(observations) >= 3 &&
			span >= math.Max(2_000, state.learnedProfile.MedianIntervalMs*2) &&
			observedMedianInterval != nil &&
			*observedMedianInterval >= state.learnedProfile.MedianIntervalMs*0.5 &&
			*observedMedianInterval <= state.learnedProfile.MedianIntervalMs*2 &&
			evidence.regularCadence
		continuousThreshold := len(observations) >= 8 && span >= 15_000 && len(distinct) >= 6 && evidence.count >= 2
		discreteThreshold := len(observations) >= 10 && span >= 30_000 && len(distinct) >= 2 && len(distinct) <= 4 && evidence.regularCadence && isDeterministicCycle(observations)
		if learnedThreshold || continuousThreshold || discreteThreshold {
			state.status = "suppressed"
			count := len(state.observations)
			state.suppressedObservationCount = &count
			state.pendingValue = nil
			state.pendingDueAt = nil
			newlySuppressed = append(newlySuppressed, state.name)
			continue
		}
		if state.status == "observing" && len(observations) >= 4 && span >= 6_000 && evidence.count >= 1 {
			state.status = "suspected"
		}
	}
	var suppressedStates []*persistVariableState
	for _, varKey := range file.order {
		state := file.variables[varKey]
		if state.status == "suppressed" && !state.cohortSuppressed {
			suppressedStates = append(suppressedStates, state)
		}
	}
	for _, varKey := range file.order {
		state := file.variables[varKey]
		if state.status == "suppressed" || len(state.observations) < 3 {
			continue
		}
		if observationSpan(state.observations) < 15_000 {
			continue
		}
		var primary *persistVariableState
		for _, candidate := range suppressedStates {
			if conditionalCochangeRate(state, candidate) >= 0.9 {
				primary = candidate
				break
			}
		}
		if primary == nil {
			continue
		}
		state.status = "suppressed"
		state.cohortSuppressed = true
		count := len(state.observations)
		state.suppressedObservationCount = &count
		state.pendingValue = nil
		state.pendingDueAt = nil
		newlySuppressed = append(newlySuppressed, state.name)
	}
	if len(suppressedStates) > 0 {
		var sparseCandidates []*persistVariableState
		for _, varKey := range file.order {
			state := file.variables[varKey]
			if state.status != "suppressed" && isSparseCycleCandidate(state) {
				sparseCandidates = append(sparseCandidates, state)
			}
		}
		for _, state := range sparseCandidates {
			var peer *persistVariableState
			for _, candidate := range sparseCandidates {
				if candidate != state && isSparseRuntimePair(state, candidate) {
					peer = candidate
					break
				}
			}
			if peer == nil {
				continue
			}
			for _, candidate := range []*persistVariableState{state, peer} {
				if candidate.status == "suppressed" {
					continue
				}
				candidate.status = "suppressed"
				candidate.cohortSuppressed = true
				candidate.sparseCohortSuppressed = true
				count := len(candidate.observations)
				candidate.suppressedObservationCount = &count
				candidate.pendingValue = nil
				candidate.pendingDueAt = nil
				newlySuppressed = append(newlySuppressed, candidate.name)
			}
		}
	}
	return newlySuppressed
}

func (l *TogglePersistLearner) evaluateLearning(file *persistFileState, at int64) []TogglePersistLearnedVariable {
	var newlyLearned []TogglePersistLearnedVariable
	for _, varKey := range file.order {
		state := file.variables[varKey]
		if state.status != "suppressed" || state.learnedInSession {
			continue
		}
		evidence := runtimeEvidence(file, state)
		individuallyLearned := !state.cohortSuppressed && len(state.observations) >= 12 && observationSpan(state.observations) >= 30_000 && evidence.count >= 3
		directCohortLearned := state.cohortSuppressed && !state.sparseCohortSuppressed && len(state.observations) >= 4 && observationSpan(state.observations) >= 30_000 &&
			hasMatchingState(file, state, func(candidate *persistVariableState) bool {
				return candidate.status == "suppressed" && !candidate.cohortSuppressed && conditionalCochangeRate(state, candidate) >= 0.9
			})
		sparseCohortLearned := state.sparseCohortSuppressed &&
			len(state.observations) >= 5 &&
			state.suppressedObservationCount != nil &&
			len(state.observations) > *state.suppressedObservationCount &&
			observationSpan(state.observations) >= 30_000 &&
			hasMatchingState(file, state, func(candidate *persistVariableState) bool {
				return candidate.status == "suppressed" && candidate.sparseCohortSuppressed && isSparseRuntimePair(state, candidate)
			}) &&
			hasMatchingState(file, state, func(candidate *persistVariableState) bool {
				return candidate.status == "suppressed" && !candidate.cohortSuppressed
			})
		if !individuallyLearned && !directCohortLearned && !sparseCohortLearned {
			continue
		}
		interval := initialQuietMs
		if median := medianInterval(state.observations); median != nil {
			interval = int(math.Round(*median))
		}
		learned := TogglePersistLearnedVariable{
			Name: state.name, MedianIntervalMs: float64(interval),
			LearnedAt: timeISO(at),
		}
		state.learnedProfile = &learned
		state.learnedInSession = true
		newlyLearned = append(newlyLearned, learned)
	}
	return newlyLearned
}

var persistValueRE = regexp.MustCompile(`(?i)^(\s*global\s+persist\s+\$[^=]+\s*=\s*).*$`)

func fingerprintTogglePersistINI(content string) string {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	inConstants := false
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			inConstants = strings.EqualFold(trimmed, "[Constants]")
		}
		if inConstants && regexp.MustCompile(`(?i)^global\s+persist\s+\$`).MatchString(trimmed) {
			out = append(out, persistValueRE.ReplaceAllString(line, "${1}<persist-value>"))
			continue
		}
		out = append(out, line)
	}
	sum := sha256.Sum256([]byte(strings.Join(out, "\n")))
	return hex.EncodeToString(sum[:])
}

func parseTogglePersistProfile(value any) (TogglePersistProfile, error) {
	record, ok := asObject(value)
	if !ok {
		return TogglePersistProfile{}, fmt.Errorf("unsupported toggle persist profile version")
	}
	version, _ := asFloat(record["version"])
	if int(version) != togglePersistProfileVersion {
		return TogglePersistProfile{}, fmt.Errorf("unsupported toggle persist profile version")
	}
	filesRaw, ok := asObject(record["files"])
	if !ok {
		return TogglePersistProfile{}, fmt.Errorf("invalid toggle persist profile files")
	}
	files := map[string]TogglePersistProfileFile{}
	for fileName, rawFile := range filesRaw {
		fileObj, ok := asObject(rawFile)
		if !ok {
			return TogglePersistProfile{}, fmt.Errorf("invalid toggle persist profile file entry: %s", fileName)
		}
		fingerprint, _ := fileObj["fingerprint"].(string)
		if fingerprint == "" {
			return TogglePersistProfile{}, fmt.Errorf("invalid toggle persist profile file entry: %s", fileName)
		}
		variablesRaw, ok := asObject(fileObj["variables"])
		if !ok {
			return TogglePersistProfile{}, fmt.Errorf("invalid toggle persist profile variables: %s", fileName)
		}
		variables := map[string]TogglePersistLearnedVariable{}
		for varName, rawVariable := range variablesRaw {
			variableObj, ok := asObject(rawVariable)
			if !ok {
				return TogglePersistProfile{}, fmt.Errorf("invalid toggle persist profile variable: %s:%s", fileName, varName)
			}
			name, _ := variableObj["name"].(string)
			median, medianOK := asFloat(variableObj["medianIntervalMs"])
			learnedAt, _ := variableObj["learnedAt"].(string)
			if name == "" || !medianOK || !isFinite(median) || median <= 0 || learnedAt == "" {
				return TogglePersistProfile{}, fmt.Errorf("invalid toggle persist profile variable: %s:%s", fileName, varName)
			}
			variables[strings.ToLower(varName)] = TogglePersistLearnedVariable{Name: name, MedianIntervalMs: median, LearnedAt: learnedAt}
		}
		files[fileName] = TogglePersistProfileFile{Fingerprint: fingerprint, Variables: variables}
	}
	return TogglePersistProfile{Version: togglePersistProfileVersion, Files: files}, nil
}

func createEmptyTogglePersistProfile() TogglePersistProfile {
	return TogglePersistProfile{Version: togglePersistProfileVersion, Files: map[string]TogglePersistProfileFile{}}
}

func togglePersistProfilePath(targetINIPath string) string {
	return filepath.Join(filepath.Dir(targetINIPath), togglePersistProfileFile)
}

type persistEvidence struct {
	count          int
	regularCadence bool
}

func runtimeEvidence(file *persistFileState, state *persistVariableState) persistEvidence {
	active := activityRate(state.observations) >= 0.6
	regularCadence := isRegularCadence(state.observations)
	continuousNumeric := isContinuousNumeric(state.observations)
	correlated := false
	for _, varKey := range file.order {
		candidate := file.variables[varKey]
		if candidate != state && isStronglyCorrelated(state, candidate) {
			correlated = true
			break
		}
	}
	count := 0
	for _, flag := range []bool{active, regularCadence, continuousNumeric, correlated} {
		if flag {
			count++
		}
	}
	return persistEvidence{count: count, regularCadence: regularCadence}
}

func activityRate(observations []persistObservation) float64 {
	if len(observations) == 0 {
		return 0
	}
	revisions := map[int]struct{}{}
	for _, observation := range observations {
		revisions[observation.revision] = struct{}{}
	}
	first := observations[0].revision
	last := observations[len(observations)-1].revision
	return float64(len(revisions)) / math.Max(1, float64(last-first+1))
}

func isRegularCadence(observations []persistObservation) bool {
	intervals := observationIntervals(observations)
	if len(intervals) < 2 {
		return false
	}
	middle := median(intervals)
	if middle == 0 {
		return false
	}
	devs := make([]float64, len(intervals))
	for i, interval := range intervals {
		devs[i] = math.Abs(interval - middle)
	}
	return median(devs)/middle <= 0.5
}

func isContinuousNumeric(observations []persistObservation) bool {
	values := make([]float64, 0, len(observations))
	for _, observation := range observations {
		if observation.numericValue == nil {
			return false
		}
		values = append(values, *observation.numericValue)
	}
	if len(values) < 6 || len(values) != len(observations) {
		return false
	}
	var nonZero []float64
	for i := 1; i < len(values); i++ {
		step := math.Abs(values[i] - values[i-1])
		if step > 0 {
			nonZero = append(nonZero, step)
		}
	}
	if len(nonZero) < 5 {
		return false
	}
	middle := median(nonZero)
	if middle <= 0 {
		return false
	}
	matched := 0
	for _, step := range nonZero {
		if step >= middle*0.25 && step <= middle*4 {
			matched++
		}
	}
	return float64(matched)/float64(len(nonZero)) >= 0.75
}

func isStronglyCorrelated(left, right *persistVariableState) bool {
	leftRevisions := map[int]struct{}{}
	rightRevisions := map[int]struct{}{}
	for _, observation := range left.observations {
		leftRevisions[observation.revision] = struct{}{}
	}
	for _, observation := range right.observations {
		rightRevisions[observation.revision] = struct{}{}
	}
	intersection := 0
	for revision := range leftRevisions {
		if _, ok := rightRevisions[revision]; ok {
			intersection++
		}
	}
	union := len(leftRevisions)
	for revision := range rightRevisions {
		if _, ok := leftRevisions[revision]; !ok {
			union++
		}
	}
	if intersection >= 4 && float64(intersection)/float64(union) >= 0.8 {
		return true
	}
	leftByRevision := map[int]*float64{}
	for _, observation := range left.observations {
		leftByRevision[observation.revision] = observation.numericValue
	}
	var pairs [][2]float64
	for _, observation := range right.observations {
		leftValue := leftByRevision[observation.revision]
		if leftValue == nil || observation.numericValue == nil {
			continue
		}
		pairs = append(pairs, [2]float64{*leftValue, *observation.numericValue})
	}
	if len(pairs) < 5 {
		return false
	}
	return math.Abs(pearsonCorrelation(pairs)) >= 0.98
}

func conditionalCochangeRate(dependent, primary *persistVariableState) float64 {
	primaryRevisions := map[int]struct{}{}
	for _, observation := range primary.observations {
		primaryRevisions[observation.revision] = struct{}{}
	}
	if len(dependent.observations) == 0 {
		return 0
	}
	dependentRevisions := map[int]struct{}{}
	shared := 0
	for _, observation := range dependent.observations {
		if _, seen := dependentRevisions[observation.revision]; seen {
			continue
		}
		dependentRevisions[observation.revision] = struct{}{}
		if _, ok := primaryRevisions[observation.revision]; ok {
			shared++
		}
	}
	return float64(shared) / float64(len(dependentRevisions))
}

func isSparseCycleCandidate(state *persistVariableState) bool {
	distinct := map[string]struct{}{}
	for _, observation := range state.observations {
		distinct[observation.value] = struct{}{}
	}
	return len(state.observations) >= 4 &&
		observationSpan(state.observations) >= 30_000 &&
		len(distinct) >= 2 && len(distinct) <= 4 &&
		isRegularCadence(state.observations) &&
		isDeterministicCycle(state.observations)
}

func isSparseRuntimePair(left, right *persistVariableState) bool {
	if !isSparseCycleCandidate(left) || !isSparseCycleCandidate(right) {
		return false
	}
	leftRevisions := map[int]struct{}{}
	rightRevisions := map[int]struct{}{}
	for _, observation := range left.observations {
		leftRevisions[observation.revision] = struct{}{}
	}
	for _, observation := range right.observations {
		rightRevisions[observation.revision] = struct{}{}
	}
	shared := 0
	for revision := range leftRevisions {
		if _, ok := rightRevisions[revision]; ok {
			shared++
		}
	}
	union := len(leftRevisions)
	for revision := range rightRevisions {
		if _, ok := leftRevisions[revision]; !ok {
			union++
		}
	}
	return float64(shared)/float64(union) >= 0.8
}

func isDeterministicCycle(observations []persistObservation) bool {
	transitions := map[string]map[string]struct{}{}
	for i := 1; i < len(observations); i++ {
		previous := observations[i-1].value
		next := observations[i].value
		if transitions[previous] == nil {
			transitions[previous] = map[string]struct{}{}
		}
		transitions[previous][next] = struct{}{}
	}
	for _, nextValues := range transitions {
		if len(nextValues) != 1 {
			return false
		}
	}
	return true
}

func quietWindow(state *persistVariableState) int64 {
	var interval *float64
	if median := medianInterval(state.observations); median != nil {
		interval = median
	} else if state.learnedProfile != nil {
		value := state.learnedProfile.MedianIntervalMs
		interval = &value
	}
	if interval == nil {
		return initialQuietMs
	}
	return int64(math.Min(maximumQuietMs, math.Max(initialQuietMs, *interval*3)))
}

func medianInterval(observations []persistObservation) *float64 {
	intervals := observationIntervals(observations)
	if len(intervals) > 5 {
		intervals = intervals[len(intervals)-5:]
	}
	if len(intervals) == 0 {
		return nil
	}
	value := median(intervals)
	return &value
}

func observationIntervals(observations []persistObservation) []float64 {
	if len(observations) < 2 {
		return nil
	}
	intervals := make([]float64, 0, len(observations)-1)
	for i := 1; i < len(observations); i++ {
		intervals = append(intervals, float64(observations[i].at-observations[i-1].at))
	}
	return intervals
}

func observationSpan(observations []persistObservation) float64 {
	if len(observations) == 0 {
		return 0
	}
	return float64(observations[len(observations)-1].at - observations[0].at)
}

func nextDueAt(file *persistFileState) *int64 {
	var due *int64
	for _, varKey := range file.order {
		state := file.variables[varKey]
		if state.pendingDueAt == nil {
			continue
		}
		if due == nil || *state.pendingDueAt < *due {
			copied := *state.pendingDueAt
			due = &copied
		}
	}
	return due
}

func median(values []float64) float64 {
	sorted := append([]float64{}, values...)
	sort.Float64s(sorted)
	middle := len(sorted) / 2
	if len(sorted)%2 == 0 {
		return (sorted[middle-1] + sorted[middle]) / 2
	}
	return sorted[middle]
}

func pearsonCorrelation(pairs [][2]float64) float64 {
	n := float64(len(pairs))
	var leftMean, rightMean float64
	for _, pair := range pairs {
		leftMean += pair[0]
		rightMean += pair[1]
	}
	leftMean /= n
	rightMean /= n
	var numerator, leftVariance, rightVariance float64
	for _, pair := range pairs {
		numerator += (pair[0] - leftMean) * (pair[1] - rightMean)
		leftVariance += (pair[0] - leftMean) * (pair[0] - leftMean)
		rightVariance += (pair[1] - rightMean) * (pair[1] - rightMean)
	}
	denominator := math.Sqrt(leftVariance * rightVariance)
	if denominator == 0 {
		return 0
	}
	return numerator / denominator
}

func parseFiniteNumber(value string) *float64 {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parsed, err := parseFloat(value)
	if err != nil || !isFinite(parsed) {
		return nil
	}
	return &parsed
}

func parseFloat(value string) (float64, error) {
	return json.Number(strings.TrimSpace(value)).Float64()
}

func isFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func fileKey(targetINIPath string) string {
	abs, err := filepath.Abs(targetINIPath)
	if err != nil {
		abs = filepath.Clean(targetINIPath)
	}
	return strings.ToLower(abs)
}

func asObject(value any) (map[string]any, bool) {
	switch typed := value.(type) {
	case map[string]any:
		return typed, true
	default:
		return nil, false
	}
}

func asFloat(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case json.Number:
		n, err := typed.Float64()
		return n, err == nil
	case int:
		return float64(typed), true
	default:
		return 0, false
	}
}

func hasMatchingState(file *persistFileState, state *persistVariableState, match func(*persistVariableState) bool) bool {
	for _, varKey := range file.order {
		candidate := file.variables[varKey]
		if candidate != state && match(candidate) {
			return true
		}
	}
	return false
}

func timeISO(at int64) string {
	return time.UnixMilli(at).UTC().Format("2006-01-02T15:04:05.000Z")
}
