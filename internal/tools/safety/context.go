package safety

import "time"

type ToolMode string

const (
	ModeNormal          ToolMode = "normal"
	ModeReadOnly        ToolMode = "read_only"
	ModePermissionAware ToolMode = "permission_aware"
)

type ToolSafetyContext struct {
	SessionRepoRoot string
	Mode            ToolMode
	CommandTimeout  time.Duration
}

func (c ToolSafetyContext) EffectiveMode() ToolMode {
	if c.Mode == "" {
		return ModeNormal
	}

	return c.Mode
}
