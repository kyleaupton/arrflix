package policy

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/repo"
)

// Engine evaluates policies against torrent metadata to produce plans
type Engine struct {
	repo   *repo.Repository
	logger *logger.Logger
}

func NewEngine(r *repo.Repository, l *logger.Logger) *Engine {
	return &Engine{repo: r, logger: l}
}

// Evaluate evaluates all enabled policies in priority order and returns an EvaluationTrace
func (e *Engine) Evaluate(ctx context.Context, evalCtx model.EvaluationContext) (model.EvaluationTrace, error) {
	trace := model.EvaluationTrace{
		Policies: []model.PolicyEvaluation{},
		FinalPlan: model.Plan{
			DownloaderID:   "",
			LibraryID:      "",
			NameTemplateID: "",
		},
		Context: e.buildContextSnapshot(evalCtx),
	}

	policies, err := e.repo.ListPolicies(ctx)
	if err != nil {
		return trace, fmt.Errorf("list policies: %w", err)
	}

	// Filter to only enabled policies
	var enabledPolicies []model.Policy
	for _, p := range policies {
		if p.Enabled {
			enabledPolicies = append(enabledPolicies, p)
		}
	}

	// Evaluate policies in priority order (already sorted DESC by query)
	for _, policy := range enabledPolicies {
		policyEval := model.PolicyEvaluation{
			PolicyID:          policy.ID.String(),
			PolicyName:        policy.Name,
			Priority:          int(policy.Priority),
			Matched:           false,
			ActionsApplied:    []model.ActionInfo{},
			StoppedProcessing: false,
		}

		// Policy without a rule doesn't match.
		if policy.Rule == nil {
			policyEval.RuleEvaluated = nil
			trace.Policies = append(trace.Policies, policyEval)
			continue
		}
		rule := *policy.Rule

		// Resolve values for the rule operands
		leftVal, _ := e.getValue(rule.LeftOperand, evalCtx)
		rightVal, _ := e.getValue(rule.RightOperand, evalCtx)

		// Store rule info with resolved values
		policyEval.RuleEvaluated = &model.RuleInfo{
			LeftOperand:        rule.LeftOperand,
			LeftResolvedValue:  leftVal,
			Operator:           rule.Operator,
			RightOperand:       rule.RightOperand,
			RightResolvedValue: rightVal,
		}

		// Evaluate rule
		matches, err := e.evaluateRule(ctx, rule, evalCtx)
		if err != nil {
			return trace, fmt.Errorf("evaluate rule for policy %s: %w", policy.ID, err)
		}

		if !matches {
			trace.Policies = append(trace.Policies, policyEval)
			continue
		}

		// Policy matched!
		policyEval.Matched = true

		// Apply actions in order and track them. The composed Policy already
		// carries the ordered Actions slice (loaded by repo.ListPolicies), so
		// no additional fetch is needed here.
		for _, action := range policy.Actions {
			actionInfo := model.ActionInfo{
				Type:  action.Type,
				Value: action.Value,
				Order: action.Order,
			}
			policyEval.ActionsApplied = append(policyEval.ActionsApplied, actionInfo)

			if err := e.applyAction(&trace.FinalPlan, action); err != nil {
				return trace, fmt.Errorf("apply action %s: %w", action.ID, err)
			}

			// Stop processing if stop_processing action
			if action.Type == string(model.ActionStopProcessing) {
				policyEval.StoppedProcessing = true
				trace.Policies = append(trace.Policies, policyEval)
				return trace, nil
			}
		}

		trace.Policies = append(trace.Policies, policyEval)
	}

	e.logger.Debug().Msgf("final plan: %+v", trace.FinalPlan)

	// Default logic
	// If we get to this point and there are decisions that are not made, we should
	// attempt to set the missing decisions to the default values. If the user does
	// not have default items set then we should return an error.
	if trace.FinalPlan.DownloaderID == "" {
		downloader, err := e.repo.GetDefaultDownloader(ctx, "torrent")
		if err != nil {
			return trace, fmt.Errorf("get default downloader: %w", err)
		}
		trace.FinalPlan.DownloaderID = downloader.ID.String()
	}

	// Determine media type from context
	mediaType := model.MediaType(evalCtx.Media.Type)
	if mediaType == "" {
		// Fallback: try to infer from categories
		for _, cat := range evalCtx.Candidate.Categories {
			if strings.HasPrefix(cat, "Movies/") || cat == "Movies" {
				mediaType = model.MediaTypeMovie
				break
			}
			if strings.HasPrefix(cat, "TV/") || cat == "TV" {
				mediaType = model.MediaTypeSeries
				break
			}
		}
	}

	if trace.FinalPlan.LibraryID == "" {
		library, err := e.repo.GetDefaultLibrary(ctx, string(mediaType))
		if err != nil {
			return trace, fmt.Errorf("get default library: %w", err)
		}
		trace.FinalPlan.LibraryID = library.ID.String()
	}

	if trace.FinalPlan.NameTemplateID == "" {
		nameTemplate, err := e.repo.GetDefaultNameTemplate(ctx, string(mediaType))
		if err != nil {
			return trace, fmt.Errorf("get default name template: %w", err)
		}
		trace.FinalPlan.NameTemplateID = nameTemplate.ID.String()
	}

	return trace, nil
}

// evaluateRule evaluates a rule against the evaluation context
func (e *Engine) evaluateRule(ctx context.Context, rule model.Rule, evalCtx model.EvaluationContext) (bool, error) {
	operator := model.Operator(rule.Operator)

	// Handle logical operators (and, or, not) which reference other rules
	switch operator {
	case model.OpAnd:
		return e.evaluateLogicalAnd(ctx, rule, evalCtx)
	case model.OpOr:
		return e.evaluateLogicalOr(ctx, rule, evalCtx)
	case model.OpNot:
		return e.evaluateLogicalNot(ctx, rule, evalCtx)
	}

	// Evaluate left operand
	leftVal, err := e.getValue(rule.LeftOperand, evalCtx)
	if err != nil {
		return false, fmt.Errorf("get left value: %w", err)
	}

	// Evaluate right operand
	rightVal, err := e.getValue(rule.RightOperand, evalCtx)
	if err != nil {
		return false, fmt.Errorf("get right value: %w", err)
	}

	// Compare based on operator
	return e.compare(leftVal, operator, rightVal)
}

// evaluateLogicalAnd evaluates an AND rule (left and right are rule UUIDs)
func (e *Engine) evaluateLogicalAnd(ctx context.Context, rule model.Rule, evalCtx model.EvaluationContext) (bool, error) {
	leftRule, err := e.getRuleByID(ctx, rule.LeftOperand)
	if err != nil {
		return false, err
	}
	rightRule, err := e.getRuleByID(ctx, rule.RightOperand)
	if err != nil {
		return false, err
	}

	leftResult, err := e.evaluateRule(ctx, leftRule, evalCtx)
	if err != nil {
		return false, err
	}
	rightResult, err := e.evaluateRule(ctx, rightRule, evalCtx)
	if err != nil {
		return false, err
	}

	return leftResult && rightResult, nil
}

// evaluateLogicalOr evaluates an OR rule (left and right are rule UUIDs)
func (e *Engine) evaluateLogicalOr(ctx context.Context, rule model.Rule, evalCtx model.EvaluationContext) (bool, error) {
	leftRule, err := e.getRuleByID(ctx, rule.LeftOperand)
	if err != nil {
		return false, err
	}
	rightRule, err := e.getRuleByID(ctx, rule.RightOperand)
	if err != nil {
		return false, err
	}

	leftResult, err := e.evaluateRule(ctx, leftRule, evalCtx)
	if err != nil {
		return false, err
	}
	rightResult, err := e.evaluateRule(ctx, rightRule, evalCtx)
	if err != nil {
		return false, err
	}

	return leftResult || rightResult, nil
}

// evaluateLogicalNot evaluates a NOT rule (right is a rule UUID)
func (e *Engine) evaluateLogicalNot(ctx context.Context, rule model.Rule, evalCtx model.EvaluationContext) (bool, error) {
	rightRule, err := e.getRuleByID(ctx, rule.RightOperand)
	if err != nil {
		return false, err
	}

	result, err := e.evaluateRule(ctx, rightRule, evalCtx)
	if err != nil {
		return false, err
	}

	return !result, nil
}

// getRuleByID gets a rule by UUID string. Logical-operator rules reference
// child rules by their UUID stored as a string in LeftOperand/RightOperand;
// the engine resolves those references lazily here at evaluation time.
func (e *Engine) getRuleByID(ctx context.Context, ruleIDStr string) (model.Rule, error) {
	ruleID, err := uuid.Parse(ruleIDStr)
	if err != nil {
		return model.Rule{}, fmt.Errorf("invalid rule UUID: %w", err)
	}
	return e.repo.GetRuleByID(ctx, ruleID)
}

// getValue gets a value from the evaluation context or returns the literal value
func (e *Engine) getValue(operand string, evalCtx model.EvaluationContext) (any, error) {
	// Check if it's a field reference using the unified context
	if strings.Contains(operand, ".") {
		parts := strings.SplitN(operand, ".", 2)
		namespace := parts[0]

		// Handle known namespaces using the unified GetField
		switch namespace {
		case "candidate", "quality", "media", "mediainfo":
			val, err := evalCtx.GetField(operand)
			if err != nil {
				// For mediainfo fields that aren't available yet, return nil gracefully
				if namespace == "mediainfo" && strings.Contains(err.Error(), "not available") {
					return nil, nil
				}
				return nil, err
			}
			return val, nil
		case "torrent":
			// Backward compatibility: support torrent.* (deprecated)
			e.logger.Warn().Str("field", operand).Msg("Using deprecated torrent.* field, please migrate to candidate.*")
			newPath := "candidate." + parts[1]
			return evalCtx.GetField(newPath)
		}
	}

	// Try to parse as number
	if num, err := strconv.ParseInt(operand, 10, 64); err == nil {
		return num, nil
	}
	if num, err := strconv.ParseFloat(operand, 64); err == nil {
		return num, nil
	}

	// Return as string
	return operand, nil
}

// compare compares two values based on operator
func (e *Engine) compare(left any, operator model.Operator, right any) (bool, error) {
	switch operator {
	case model.OpEq:
		return e.equals(left, right), nil
	case model.OpNe:
		return !e.equals(left, right), nil
	case model.OpGt:
		return e.greaterThan(left, right)
	case model.OpGte:
		gt, err := e.greaterThan(left, right)
		if err != nil {
			return false, err
		}
		eq := e.equals(left, right)
		return gt || eq, nil
	case model.OpLt:
		return e.lessThan(left, right)
	case model.OpLte:
		lt, err := e.lessThan(left, right)
		if err != nil {
			return false, err
		}
		eq := e.equals(left, right)
		return lt || eq, nil
	case model.OpContains:
		return e.contains(left, right)
	case model.OpIn:
		return e.in(left, right)
	case model.OpNotIn:
		result, err := e.in(left, right)
		return !result, err
	default:
		return false, fmt.Errorf("unsupported operator: %s", operator)
	}
}

func (e *Engine) equals(left, right any) bool {
	return fmt.Sprintf("%v", left) == fmt.Sprintf("%v", right)
}

func (e *Engine) greaterThan(left, right any) (bool, error) {
	leftNum, rightNum, err := e.toNumbers(left, right)
	if err != nil {
		return false, err
	}
	return leftNum > rightNum, nil
}

func (e *Engine) lessThan(left, right any) (bool, error) {
	leftNum, rightNum, err := e.toNumbers(left, right)
	if err != nil {
		return false, err
	}
	return leftNum < rightNum, nil
}

func (e *Engine) toNumbers(left, right any) (float64, float64, error) {
	leftNum, ok := e.toFloat64(left)
	if !ok {
		return 0, 0, fmt.Errorf("left operand is not a number: %v", left)
	}
	rightNum, ok := e.toFloat64(right)
	if !ok {
		return 0, 0, fmt.Errorf("right operand is not a number: %v", right)
	}
	return leftNum, rightNum, nil
}

func (e *Engine) toFloat64(v any) (float64, bool) {
	switch val := v.(type) {
	case int64:
		return float64(val), true
	case float64:
		return val, true
	case int:
		return float64(val), true
	case uint:
		return float64(val), true
	case uint64:
		return float64(val), true
	default:
		return 0, false
	}
}

func (e *Engine) contains(left, right any) (bool, error) {
	leftStr := fmt.Sprintf("%v", left)
	rightStr := fmt.Sprintf("%v", right)
	return strings.Contains(leftStr, rightStr), nil
}

func (e *Engine) in(left, right any) (bool, error) {
	leftStr := fmt.Sprintf("%v", left)

	// Right should be a comma-separated list or array
	rightStr := fmt.Sprintf("%v", right)
	values := strings.Split(rightStr, ",")
	for _, v := range values {
		if strings.TrimSpace(v) == leftStr {
			return true, nil
		}
	}

	// Also check if right is a slice
	if categories, ok := right.([]string); ok {
		for _, cat := range categories {
			if cat == leftStr {
				return true, nil
			}
		}
	}

	return false, nil
}

// applyAction applies an action to the plan
func (e *Engine) applyAction(plan *model.Plan, action model.Action) error {
	actionType := model.ActionType(action.Type)

	switch actionType {
	case model.ActionSetDownloader:
		plan.DownloaderID = action.Value
	case model.ActionSetLibrary:
		plan.LibraryID = action.Value
	case model.ActionSetNameTemplate:
		plan.NameTemplateID = action.Value
	case model.ActionStopProcessing:
		// Handled in Evaluate
	default:
		return fmt.Errorf("unknown action type: %s", actionType)
	}

	return nil
}

// buildContextSnapshot creates a JSON-friendly snapshot of the evaluation context
func (e *Engine) buildContextSnapshot(evalCtx model.EvaluationContext) *model.ContextSnapshot {
	snapshot := &model.ContextSnapshot{
		Candidate: map[string]any{
			"size":         evalCtx.Candidate.Size,
			"title":        evalCtx.Candidate.Title,
			"indexer":      evalCtx.Candidate.Indexer,
			"indexer_id":   evalCtx.Candidate.IndexerID,
			"categories":   evalCtx.Candidate.Categories,
			"protocol":     evalCtx.Candidate.Protocol,
			"seeders":      evalCtx.Candidate.Seeders,
			"peers":        evalCtx.Candidate.Peers,
			"age":          evalCtx.Candidate.Age,
			"age_hours":    evalCtx.Candidate.AgeHours,
			"grabs":        evalCtx.Candidate.Grabs,
			"publish_date": evalCtx.Candidate.PublishDate,
			"link":         evalCtx.Candidate.Link,
			"guid":         evalCtx.Candidate.GUID,
		},
		Quality: map[string]any{
			"full":       evalCtx.Quality.Full,
			"resolution": evalCtx.Quality.Resolution,
			"source":     evalCtx.Quality.Source,
			"is_remux":   evalCtx.Quality.IsRemux,
			"is_repack":  evalCtx.Quality.IsRepack,
			"version":    evalCtx.Quality.Version,
		},
		Release: map[string]any{
			"release_group": evalCtx.Release.ReleaseGroup,
			"edition":       evalCtx.Release.Edition,
		},
		Media: map[string]any{
			"type":    evalCtx.Media.Type,
			"title":   evalCtx.Media.Title,
			"year":    evalCtx.Media.Year,
			"tmdb_id": evalCtx.Media.TmdbID,
		},
	}

	// Add optional media fields
	if evalCtx.Media.Season != nil {
		snapshot.Media["season"] = *evalCtx.Media.Season
	}
	if evalCtx.Media.Episode != nil {
		snapshot.Media["episode"] = *evalCtx.Media.Episode
	}
	if evalCtx.Media.EpisodeTitle != nil {
		snapshot.Media["episode_title"] = *evalCtx.Media.EpisodeTitle
	}

	// Add mediainfo if available
	if evalCtx.MediaInfo != nil {
		snapshot.MediaInfo = map[string]any{
			"video_codec":     evalCtx.MediaInfo.VideoCodec,
			"video_bit_depth": evalCtx.MediaInfo.VideoBitDepth,
			"audio_codec":     evalCtx.MediaInfo.AudioCodec,
			"audio_channels":  evalCtx.MediaInfo.AudioChannels,
			"container":       evalCtx.MediaInfo.Container,
			"duration":        evalCtx.MediaInfo.Duration,
			"file_size":       evalCtx.MediaInfo.FileSize,
			"hdr":             evalCtx.MediaInfo.HDR,
		}
	}

	return snapshot
}
