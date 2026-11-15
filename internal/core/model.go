package core

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"time"
)

const (
	StageDemand   = "明确需求"
	StagePlan     = "制定方案"
	StageBuild    = "实现与自验"
	StageAccept   = "验收与交付"
	StageRelease  = "发布与观察"
	StageRetro    = "复盘与改进"
	StatusActive  = "active"
	StatusBlocked = "blocked"
	StatusCancel  = "cancelled"
	StatusClosed  = "closed"
)

var StageOrder = []string{StageDemand, StagePlan, StageBuild, StageAccept, StageRelease, StageRetro}

var layerRank = map[string]int{
	"static":           1,
	"unit":             2,
	"integration":      3,
	"interaction":      4,
	"release-readback": 5,
}

const (
	ExitOK       = 0
	ExitInternal = 1
	ExitUsage    = 2
	ExitGate     = 3
	ExitCheck    = 4
	ExitBlocked  = 5
)

type Project struct {
	Schema       int           `json:"schema"`
	Environment  string        `json:"environment"`
	Release      bool          `json:"release"`
	Profile      string        `json:"profile"`
	Checks       []CheckSpec   `json:"checks"`
	Requirements []Requirement `json:"requirements"`
}

type CheckSpec struct {
	Name     string   `json:"name"`
	Layer    string   `json:"layer"`
	When     string   `json:"when"`
	Argv     []string `json:"argv"`
	Required bool     `json:"required"`
}

type Requirement struct {
	ID          string `json:"id"`
	Claim       string `json:"claim"`
	MinLayer    string `json:"min_layer"`
	Required    bool   `json:"required"`
	Check       string `json:"check"`
	Independent bool   `json:"independent"`
}

type State struct {
	Changes []Change `json:"changes"`
}

type Change struct {
	ID              string       `json:"id"`
	Goal            string       `json:"goal"`
	Stage           string       `json:"stage"`
	Status          string       `json:"status"`
	Release         bool         `json:"release"`
	Profile         string       `json:"profile"`
	Environment     string       `json:"environment"`
	Generation      int          `json:"generation"`
	Artifacts       []Artifact   `json:"artifacts"`
	Runs            []CheckRun   `json:"runs"`
	Problems        []Problem    `json:"problems"`
	Accepts         []Accept     `json:"accepts"`
	DeclaredVersion *VersionFact `json:"declared_version,omitempty"`
	ToolVersion     *VersionFact `json:"tool_version,omitempty"`
	Block           *BlockInfo   `json:"block,omitempty"`
	History         []Event      `json:"history"`
	CreatedAt       time.Time    `json:"created_at"`
}

type Artifact struct {
	Stage     string        `json:"stage"`
	Revisions []ArtifactRev `json:"revisions"`
}

type ArtifactRev struct {
	At         time.Time       `json:"at"`
	Hash       string          `json:"hash"`
	Body       json.RawMessage `json:"body"`
	Supersedes string          `json:"supersedes,omitempty"`
}

type CheckRun struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Layer         string    `json:"layer"`
	When          string    `json:"when"`
	Argv          []string  `json:"argv"`
	Cwd           string    `json:"cwd"`
	ExitCode      *int      `json:"exit_code"`
	Status        string    `json:"status"`
	Started       time.Time `json:"started"`
	Ended         time.Time `json:"ended"`
	LogPath       string    `json:"log_path,omitempty"`
	LogBytes      int       `json:"log_bytes"`
	Preview       string    `json:"preview,omitempty"`
	Fingerprint   string    `json:"fingerprint"`
	Generation    int       `json:"generation"`
	CandidateKind string    `json:"candidate_kind"`
	Note          string    `json:"note,omitempty"`
}

type Problem struct {
	ID          string `json:"id"`
	Summary     string `json:"summary"`
	Repro       string `json:"repro"`
	Expected    string `json:"expected"`
	Actual      string `json:"actual"`
	Disposition string `json:"disposition"`
	Reason      string `json:"reason"`
	Defense     string `json:"defense"`
	Scope       string `json:"scope"`
	FailRun     string `json:"fail_run,omitempty"`
	PassRun     string `json:"pass_run,omitempty"`
}

type Accept struct {
	What       string    `json:"what"`
	Source     string    `json:"source"`
	Reason     string    `json:"reason"`
	Scope      string    `json:"scope"`
	At         time.Time `json:"at"`
	Voided     bool      `json:"voided"`
	Generation int       `json:"generation"`
}

type VersionFact struct {
	Value  string    `json:"value"`
	Source string    `json:"source"`
	At     time.Time `json:"at"`
}

type BlockInfo struct {
	Reason string    `json:"reason"`
	At     time.Time `json:"at"`
}

type Event struct {
	At      time.Time `json:"at"`
	Kind    string    `json:"kind"`
	Message string    `json:"message"`
}

type Fingerprint struct {
	Kind  string `json:"kind"`
	Hash  string `json:"hash"`
	Head  string `json:"head,omitempty"`
	Note  string `json:"note,omitempty"`
	Dirty bool   `json:"dirty"`
}

func newID(prefix string) string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

func stageIndex(name string) int {
	for i, s := range StageOrder {
		if s == name {
			return i
		}
	}
	return -1
}
