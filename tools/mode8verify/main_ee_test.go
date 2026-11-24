//go:build ee

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// rootWithSpawn builds the minimal parent thread the agent probe expects: a dialog model and
// effort, plus one spawn_agent call with the given overrides.
func rootWithSpawn(model, effort, forkTurns, spawnModel, spawnEffort string) *thread {
	return &thread{
		ID:              "root1",
		Model:           model,
		ReasoningEffort: effort,
		Tools:           map[string]int{},
		Spawns: []spawnArgs{{
			TaskName:        "quantum_core",
			ForkTurns:       forkTurns,
			Model:           spawnModel,
			ReasoningEffort: spawnEffort,
		}},
	}
}

// inheritedChild is a sub-agent that ran a retrieval tool and matched the root's
// model/effort — the only shape the probe accepts.
func inheritedChild(model, effort string) *thread {
	return &thread{
		ID:              "child1",
		Parent:          "root1",
		AgentPath:       "/root/quantum_core",
		Sub:             true,
		Model:           model,
		ReasoningEffort: effort,
		Tools:           map[string]int{"mcp__ragflow__search_bm25_chunks": 1},
	}
}

func TestAgentProbeCheckAcceptsInheritedChild(t *testing.T) {
	err := agentProbeCheck([]*thread{
		rootWithSpawn("MiniMax-M3", "high", "none", "", ""),
		inheritedChild("MiniMax-M3", "high"),
	}, "")
	if err == nil || !strings.HasPrefix(err.Error(), "ok:") {
		t.Fatalf("want an ok verdict, got %v", err)
	}
}

func TestAgentProbeCheckRejectsBadSpawnArguments(t *testing.T) {
	cases := []struct {
		name       string
		forkTurns  string
		spawnModel string
		spawnEff   string
		want       string
	}{
		{"fork all", "all", "", "", "fork_turns=all"},
		{"fork omitted", "", "", "", "omitted"},
		{"model override", "none", "gpt-5.6-sol", "", "passed model"},
		{"effort override", "none", "", "low", "passed reasoning_effort"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := agentProbeCheck([]*thread{
				rootWithSpawn("MiniMax-M3", "high", tc.forkTurns, tc.spawnModel, tc.spawnEff),
				inheritedChild("MiniMax-M3", "high"),
			}, "")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestAgentProbeCheckRejectsChildDrift(t *testing.T) {
	root := rootWithSpawn("MiniMax-M3", "high", "none", "", "")

	modelDrift := inheritedChild("gpt-5.6-sol", "high")
	if err := agentProbeCheck([]*thread{root, modelDrift}, ""); err == nil || !strings.Contains(err.Error(), "ran model") {
		t.Fatalf("want a model-drift error, got %v", err)
	}

	effortDrift := inheritedChild("MiniMax-M3", "low")
	if err := agentProbeCheck([]*thread{root, effortDrift}, ""); err == nil || !strings.Contains(err.Error(), "reasoning_effort") {
		t.Fatalf("want an effort-drift error, got %v", err)
	}
}

func TestAgentProbeCheckRejectsMissingHandoff(t *testing.T) {
	root := rootWithSpawn("MiniMax-M3", "high", "none", "", "")

	idle := inheritedChild("MiniMax-M3", "high")
	idle.Tools = map[string]int{"exec_command": 2}
	if err := agentProbeCheck([]*thread{root, idle}, ""); err == nil || !strings.Contains(err.Error(), "called no retrieval tool") {
		t.Fatalf("want a no-retrieval error, got %v", err)
	}

	if err := agentProbeCheck([]*thread{root}, ""); err == nil || !strings.Contains(err.Error(), "no sub-agent thread") {
		t.Fatalf("want a no-sub-agent error, got %v", err)
	}

	orphan := inheritedChild("MiniMax-M3", "high")
	if err := agentProbeCheck([]*thread{orphan}, ""); err == nil || !strings.Contains(err.Error(), "no root thread recorded") {
		t.Fatalf("want a no-spawn error, got %v", err)
	}
}

// TestLoadThreadReadsSpawnAndTurnContext pins the rollout parsing the probe relies on: the
// parent's spawn arguments (fork/model/effort) and the turn_context's effective settings.
func TestLoadThreadReadsSpawnAndTurnContext(t *testing.T) {
	root := writeRollout(t, "root.jsonl",
		`{"type":"session_meta","payload":{"id":"root1","source":"vscode"}}`,
		`{"type":"turn_context","payload":{"model":"MiniMax-M3","effort":"high"}}`,
		`{"type":"response_item","payload":{"type":"function_call","name":"spawn_agent","arguments":"{\"task_name\":\"quantum_core\",\"fork_turns\":\"none\"}"}}`,
	)
	if root.Sub {
		t.Fatal("the root rollout must not be classified as a sub-agent")
	}
	if root.Model != "MiniMax-M3" || root.ReasoningEffort != "high" {
		t.Fatalf("turn_context not read: model=%q effort=%q", root.Model, root.ReasoningEffort)
	}
	if len(root.Spawns) != 1 {
		t.Fatalf("want 1 spawn call, got %d", len(root.Spawns))
	}
	if s := root.Spawns[0]; s.TaskName != "quantum_core" || s.ForkTurns != "none" || s.Model != "" || s.ReasoningEffort != "" {
		t.Fatalf("spawn args not read: %+v", s)
	}

	child := writeRollout(t, "child.jsonl",
		`{"type":"session_meta","payload":{"id":"child1","source":{"subagent":{"thread_spawn":{"parent_thread_id":"root1","agent_path":"/root/quantum_core"}}}}}`,
		`{"type":"turn_context","payload":{"model":"MiniMax-M3","effort":"high"}}`,
	)
	if !child.Sub || child.Parent != "root1" || child.AgentPath != "/root/quantum_core" {
		t.Fatalf("child spawn edge not read: sub=%v parent=%q path=%q", child.Sub, child.Parent, child.AgentPath)
	}
}

// TestRecordSpawnKeepsOmittedArguments pins that an unparseable/empty spawn call still
// records an entry, so a probe reads an omitted fork_turns as "not requested" rather than
// as "no call at all".
func TestRecordSpawnKeepsOmittedArguments(t *testing.T) {
	var th thread
	th.recordSpawn("")
	if len(th.Spawns) != 1 || th.Spawns[0].ForkTurns != "" {
		t.Fatalf("want one empty spawn entry, got %+v", th.Spawns)
	}
}

func writeRollout(t *testing.T, name string, lines ...string) *thread {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("write rollout: %v", err)
	}
	th, err := loadThread(path)
	if err != nil {
		t.Fatalf("loadThread: %v", err)
	}
	return th
}
