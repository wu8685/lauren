package skillbundle

import _ "embed"

//go:embed SKILL.md
var SkillMD []byte

//go:embed plugin.json
var PluginJSON []byte
