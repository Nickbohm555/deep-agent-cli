package safety

import "testing"

func TestPolicyMatrix(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		mode     ToolMode
		action   ToolAction
		expected Decision
	}{
		{name: "default mode allows read file", mode: "", action: ActionReadFile, expected: DecisionAllow},
		{name: "normal mode allows read file", mode: ModeNormal, action: ActionReadFile, expected: DecisionAllow},
		{name: "normal mode allows list files", mode: ModeNormal, action: ActionListFiles, expected: DecisionAllow},
		{name: "normal mode allows bash execute", mode: ModeNormal, action: ActionBashExecute, expected: DecisionAllow},
		{name: "normal mode allows code search", mode: ModeNormal, action: ActionCodeSearch, expected: DecisionAllow},
		{name: "read only mode allows read file", mode: ModeReadOnly, action: ActionReadFile, expected: DecisionAllow},
		{name: "read only mode allows list files", mode: ModeReadOnly, action: ActionListFiles, expected: DecisionAllow},
		{name: "read only mode dry runs bash execute", mode: ModeReadOnly, action: ActionBashExecute, expected: DecisionDryRun},
		{name: "read only mode allows code search", mode: ModeReadOnly, action: ActionCodeSearch, expected: DecisionAllow},
		{name: "permission aware mode allows read file", mode: ModePermissionAware, action: ActionReadFile, expected: DecisionAllow},
		{name: "permission aware mode allows list files", mode: ModePermissionAware, action: ActionListFiles, expected: DecisionAllow},
		{name: "permission aware mode requires approval for bash execute", mode: ModePermissionAware, action: ActionBashExecute, expected: DecisionRequireApproval},
		{name: "permission aware mode allows code search", mode: ModePermissionAware, action: ActionCodeSearch, expected: DecisionAllow},
		{name: "unknown action is denied", mode: ModeNormal, action: ToolAction("unknown"), expected: DecisionDeny},
		{name: "unknown mode is denied", mode: ToolMode("custom"), action: ActionReadFile, expected: DecisionDeny},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := EvaluateAction(tc.mode, tc.action); got != tc.expected {
				t.Fatalf("EvaluateAction(%q, %q) = %q, want %q", tc.mode, tc.action, got, tc.expected)
			}
		})
	}
}
