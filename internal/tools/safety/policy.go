package safety

type ToolAction string

const (
	ActionReadFile    ToolAction = "read_file"
	ActionListFiles   ToolAction = "list_files"
	ActionBashExecute ToolAction = "bash_execute"
	ActionCodeSearch  ToolAction = "code_search"
)

type Decision string

const (
	DecisionAllow           Decision = "allow"
	DecisionDeny            Decision = "deny"
	DecisionRequireApproval Decision = "require_approval"
	DecisionDryRun          Decision = "dry_run"
)

var modePolicyMatrix = map[ToolMode]map[ToolAction]Decision{
	ModeNormal: {
		ActionReadFile:    DecisionAllow,
		ActionListFiles:   DecisionAllow,
		ActionBashExecute: DecisionAllow,
		ActionCodeSearch:  DecisionAllow,
	},
	ModeReadOnly: {
		ActionReadFile:    DecisionAllow,
		ActionListFiles:   DecisionAllow,
		ActionBashExecute: DecisionDryRun,
		ActionCodeSearch:  DecisionAllow,
	},
	ModePermissionAware: {
		ActionReadFile:    DecisionAllow,
		ActionListFiles:   DecisionAllow,
		ActionBashExecute: DecisionRequireApproval,
		ActionCodeSearch:  DecisionAllow,
	},
}

func EvaluateAction(mode ToolMode, action ToolAction) Decision {
	if decisions, ok := modePolicyMatrix[normalizeMode(mode)]; ok {
		if decision, ok := decisions[action]; ok {
			return decision
		}
	}

	return DecisionDeny
}

func normalizeMode(mode ToolMode) ToolMode {
	if mode == "" {
		return ModeNormal
	}

	return mode
}
