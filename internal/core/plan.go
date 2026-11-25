package core

import (
	"fmt"
	"strings"
	"time"
)

type includeArgs []string

func (s *includeArgs) String() string { return strings.Join(*s, ",") }
func (s *includeArgs) Set(v string) error {
	*s = append(*s, strings.TrimSpace(v))
	return nil
}

type skipArgs []string

func (s *skipArgs) String() string { return strings.Join(*s, ",") }
func (s *skipArgs) Set(v string) error {
	*s = append(*s, v)
	return nil
}

func parseSkip(v string) (string, string, error) {
	name, reason, ok := strings.Cut(v, "=")
	name = strings.TrimSpace(name)
	reason = strings.TrimSpace(reason)
	if !ok || name == "" || reason == "" {
		return "", "", fmt.Errorf("--skip 的格式是 名称=理由")
	}
	return name, reason, nil
}

func (p Project) freezePlan(skips []string) (*CheckPlan, error) {
	plan := &CheckPlan{TimeoutSeconds: p.projectTimeoutSeconds()}
	seen := map[string]bool{}
	for _, ch := range p.Checks {
		if seen[ch.Name] {
			return nil, fmt.Errorf("检查 %s 重名", ch.Name)
		}
		seen[ch.Name] = true
		stamped := ch
		stamped.TimeoutSeconds = p.effectiveTimeoutSeconds(ch)
		plan.Authorized = append(plan.Authorized, stamped)
		plan.Checks = append(plan.Checks, stamped)
	}
	for _, raw := range skips {
		name, reason, err := parseSkip(raw)
		if err != nil {
			return nil, err
		}
		if err := plan.skip(name, reason); err != nil {
			return nil, err
		}
	}
	return plan, nil
}

func (plan *CheckPlan) authorized(name string) *CheckSpec {
	for i := range plan.Authorized {
		if plan.Authorized[i].Name == name {
			return &plan.Authorized[i]
		}
	}
	return nil
}

func (plan *CheckPlan) skip(name, reason string) error {
	spec := plan.authorized(name)
	if spec == nil {
		return fmt.Errorf("检查 %s 不在这次变更授权的检查里", name)
	}
	if spec.Required {
		return fmt.Errorf("必过检查 %s 不能从计划里拿掉", name)
	}
	if strings.TrimSpace(reason) == "" {
		return fmt.Errorf("跳过可选检查 %s 需要理由", name)
	}
	var next []CheckSpec
	for _, ch := range plan.Checks {
		if ch.Name != name {
			next = append(next, ch)
		}
	}
	plan.Checks = next
	for i := range plan.Skips {
		if plan.Skips[i].Name == name {
			plan.Skips[i].Reason = reason
			return nil
		}
	}
	plan.Skips = append(plan.Skips, SkippedCheck{Name: name, Reason: reason})
	return nil
}

func (plan *CheckPlan) include(name string) error {
	spec := plan.authorized(name)
	if spec == nil {
		return fmt.Errorf("检查 %s 不在这次变更授权的检查里，不能临时加未授权命令", name)
	}
	for _, ch := range plan.Checks {
		if ch.Name == name {
			return nil
		}
	}
	plan.Checks = append(plan.Checks, *spec)
	var skips []SkippedCheck
	for _, sk := range plan.Skips {
		if sk.Name != name {
			skips = append(skips, sk)
		}
	}
	plan.Skips = skips
	return nil
}

func (plan *CheckPlan) clone() *CheckPlan {
	if plan == nil {
		return nil
	}
	out := *plan
	out.Authorized = append([]CheckSpec{}, plan.Authorized...)
	out.Checks = append([]CheckSpec{}, plan.Checks...)
	out.Skips = append([]SkippedCheck{}, plan.Skips...)
	return &out
}

func (plan *CheckPlan) apply(prop PlanProposal) error {
	for _, sk := range prop.Skip {
		if err := plan.skip(sk.Name, sk.Reason); err != nil {
			return err
		}
	}
	for _, name := range prop.Include {
		if err := plan.include(name); err != nil {
			return err
		}
	}
	return nil
}

func (c *Change) planLocked() bool {
	return stageIndex(c.Stage) > stageIndex(StageDemand)
}

func (c *Change) checks(p Project) []CheckSpec {
	if c != nil && c.Plan != nil {
		return c.Plan.Checks
	}
	return p.Checks
}

func projectWithPlan(p Project, plan *CheckPlan) Project {
	if plan == nil {
		return p
	}
	view := p
	view.Checks = append([]CheckSpec{}, plan.Checks...)
	view.Defaults.CheckTimeoutSeconds = plan.TimeoutSeconds
	return view
}

func (c *Change) projectView(p Project) Project {
	if c == nil {
		return p
	}
	return projectWithPlan(p, c.Plan)
}

func (c *Change) notePlan(at time.Time, msg string) {
	c.History = append(c.History, Event{At: at, Kind: "plan", Message: msg})
}
