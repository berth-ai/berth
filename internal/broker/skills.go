package broker

import (
	"maps"
	"slices"

	"github.com/berth-ai/berth/internal/config"
	"github.com/berth-ai/berth/internal/skills"
)

// skillsSyncGroup serializes skills.sync runs; skills.add and
// skills.update use skillsGroup, so a resolve can run during a sync.
const skillsSyncGroup = "skills-sync"

func sameSkills(a, b map[string]config.Skill) bool {
	return maps.EqualFunc(a, b, func(x, y config.Skill) bool {
		return x.Repo == y.Repo && x.Path == y.Path && x.Ref == y.Ref && x.Commit == y.Commit &&
			x.Description == y.Description && x.Enabled == y.Enabled &&
			(x.Agents == nil) == (y.Agents == nil) && slices.Equal(x.Agents, y.Agents)
	})
}

// writeSkillsSettings publishes s to SkillsSettings, readable by the agent group.
func (b *Broker) writeSkillsSettings(s map[string]config.Skill) error {
	if b.SkillsSettings == "" {
		return nil
	}
	uid, gid := -1, -1
	if b.Agent != nil {
		uid, gid = 0, int(b.Agent.GID)
	}
	return config.WriteSkillsFile(b.SkillsSettings, s, uid, gid)
}

// skillsStale reports whether SkillsSettings differs from s.
func (b *Broker) skillsStale(s map[string]config.Skill) bool {
	if b.SkillsSettings == "" {
		return false
	}
	pub, err := config.ReadSkillsFile(b.SkillsSettings)
	return err != nil || !sameSkills(pub, s)
}

// startSkillsSync runs the skills converger as a job.
func (b *Broker) startSkillsSync() {
	if b.Layer == nil || len(b.SkillsSync) == 0 {
		return
	}
	work := b.agentWork(b.SkillsSync[0], b.SkillsSync[1:], skills.SyncTimeout)
	if err := b.skillsRuns.start(b.Jobs, "skills.sync", skillsSyncGroup, work, "Skills changed during the run; running again."); err != nil {
		b.logf("WARNING: skills sync not started: %v", err)
	}
}
