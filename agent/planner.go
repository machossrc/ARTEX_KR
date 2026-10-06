package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/intercept"
	"github.com/Autumn-27/norma/agentcore"
	"github.com/Autumn-27/norma/llm"
	"github.com/Autumn-27/norma/permission"
	actool "github.com/Autumn-27/norma/tool"
	"github.com/Autumn-27/norma/transcript"
)

// Planner is the event-driven LLM planner (docs §4.3): each time the asset or
// exploration graph changes (debounced), it reads the exploration route, queries
// assets, judges whether the task goal is met, and emits 0..N exploration intents
// into the frontier. It is the sole intent generator.
type Planner struct {
	findingRecorder   FindingRecorder
	prov              llm.Provider
	model             string
	tx                *transcript.Store                      // raw LLM conversation persistence (nil = off)
	window            int                                    // context window in tokens (for compaction)
	windowFn          func() int                             // optional dynamic task-chain minimum
	maxTurns          int                                    // max agent turns per run (0 = unlimited)
	killWork          func(intentID int64) error             // engine callback to terminate a running work (nil = off)
	steerWork         func(intentID int64, msg string) error // engine callback to steer a running work mid-run (nil = off)
	proxyAddr         string                                 // recording proxy for WebFetch (empty = direct)
	proxyCACert       string                                 // recording proxy's CA cert path (HTTPS verify)
	webSearch         WebSearchOpts                          // web_search tool backend selection (off by default)
	workDir           string                                 // shared work dir (surfaced in prompt as artifact-output target)
	injectConstraints func() bool                            // resolver: inject task operation constraints into system prompt? (nil = yes)
	nonStreamingFn    func() bool                            // resolver: use non-streaming (Complete) path? (nil = streaming)
	noaEnabledFn      func() bool                            // resolver: use experimental noa compaction? (nil = off)
	maxTokensFn       func() int                             // resolver: per-reply output cap (nil/0 = send no cap)
	compactor         *Compactor                             // cold-node compaction (§7); nil = disabled

	// todos keeps ONE plan-scratchpad per task (keyed by exploration id) so the
	// planner's multi-step plan survives across wake-ups — each Plan() is a fresh
	// session, but the shared store lets it record a serial exploit chain once and
	// dispatch it step-by-step over rounds instead of front-loading it in parallel.
	todoMu sync.Mutex
	todos  map[int64]*actool.TodoStore
}

func NewPlanner(prov llm.Provider, model, workDir string, tx *transcript.Store, window, maxTurns int) *Planner {
	return &Planner{prov: prov, model: model, workDir: workDir, tx: tx, window: window, maxTurns: maxTurns, todos: map[int64]*actool.TodoStore{}}
}

func (p *Planner) SetCompactionWindowResolver(fn func() int) { p.windowFn = fn }

// SetCompactor wires the cold-node compactor (cold-digest §7). Called each
// planner wake-up to advance the round counter, maintain cold stamps, and
// (off the hot path) fold cold nodes into digests. nil = feature disabled.
func (p *Planner) SetCompactor(c *Compactor) { p.compactor = c }

// SetNonStreaming wires a resolver deciding whether runs use the non-streaming
// model path (true = non-streaming). nil/unset = streaming (default).
func (p *Planner) SetNonStreaming(fn func() bool) { p.nonStreamingFn = fn }

func (p *Planner) nonStreaming() bool { return p.nonStreamingFn != nil && p.nonStreamingFn() }

// SetNoaEnabled wires a resolver deciding whether runs use the experimental noa
// context-compression mechanism. nil/unset = off (built-in compaction). Read per
// run so the settings toggle takes effect without rebuilding the agent.
func (p *Planner) SetNoaEnabled(fn func() bool) { p.noaEnabledFn = fn }

// SetMaxTokens wires a resolver for the per-reply output cap. nil/unset or 0 =
// send no cap and let the endpoint decide. Read per run, like nonStreaming.
func (p *Planner) SetMaxTokens(fn func() int) { p.maxTokensFn = fn }

func (p *Planner) maxTokens() int {
	if p.maxTokensFn == nil {
		return 0
	}
	return p.maxTokensFn()
}

func (p *Planner) compactionWindow() int {
	if p.windowFn != nil {
		return p.windowFn()
	}
	return p.window
}

// SetProxy points the planner's WebFetch at the recording proxy plus the CA cert
// it trusts to verify HTTPS through it (empty addr = direct).
func (p *Planner) SetProxy(addr, caCert string) { p.proxyAddr, p.proxyCACert = addr, caCert }

// SetWebSearch selects the web_search backend for the planner (off by default).
func (p *Planner) SetWebSearch(o WebSearchOpts) { p.webSearch = o }

// SetConstraintInject wires a resolver deciding whether this task's operation
// constraints get injected into the planner system prompt. Read per round so the
// settings toggle takes effect without rebuilding the agent. nil = inject (default).
func (p *Planner) SetConstraintInject(fn func() bool) { p.injectConstraints = fn }

// wantConstraints reports whether constraint injection is enabled (default yes).
func (p *Planner) wantConstraints() bool { return p.injectConstraints == nil || p.injectConstraints() }

// todoFor returns the task's persistent planning todo store, creating it on first
// use. Shared across all of this task's planner wake-ups.
func (p *Planner) todoFor(expID int64) *actool.TodoStore {
	p.todoMu.Lock()
	defer p.todoMu.Unlock()
	s := p.todos[expID]
	if s == nil {
		s = actool.NewTodoStore()
		p.todos[expID] = s
	}
	return s
}

// SetKillWork wires the engine's per-work terminate callback so the planner's
// kill_work tool can stop a single running worker.
func (p *Planner) SetKillWork(fn func(intentID int64) error) { p.killWork = fn }

// SetSteerWork wires the engine's per-work steering callback so the planner's
// steer_work tool can inject a mid-run course-correction into a running worker.
func (p *Planner) SetSteerWork(fn func(intentID int64, msg string) error) { p.steerWork = fn }

// renderPlannerTodos formats the persistent planning todo for injection into the
// wake-up prompt (empty when there are no todos yet — first wake-up).
func renderPlannerTodos(items []actool.Todo) string {
	if len(items) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n【당신의 계획 할 일 목록(여러 호출 사이에 보존되며 이전 턴에 작성한 내용)】: \n")
	for _, it := range items {
		mark := map[actool.TodoStatus]string{actool.TodoPending: "☐", actool.TodoInProgress: "▶", actool.TodoCompleted: "✔"}[it.Status]
		if mark == "" {
			mark = "☐"
		}
		b.WriteString(fmt.Sprintf("  %s %s\n", mark, it.Content))
	}
	b.WriteString("이를 바탕으로 진행하세요. 【선행 단계가 완료되었거나 필요한 fact가 이미 존재하는】 다음 단계에만 의도를 배정하세요. TodoWrite로 목록을 갱신하고 fact로 충족된 단계는 completed로 표시하세요. 목록에서 이미 pending/in_progress인 단계를 중복 배정하지 마세요.")
	return b.String()
}

// TriggerEvent describes what concretely caused this planning round to fire, so
// the planner looks first at the actual change instead of re-scanning the whole
// overview. Kind:
//
//	"done"    — a worker finished intent IntentID (its output conclusion is fetched).
//	"finding" — a worker reported a finding on intent IntentID (Detail = 摘要).
//	"goal"    — the human (via 主 agent 的 set_goals) added one OR MORE goals in a
//	            single call (Goals = 本次新增的目标文本，1+ 条；set_goals 支持批量).
//	"goal_deleted" — the human deleted a goal from 总览的目标管理 (Detail = 被删目标文本).
//	"goal_edited"  — the human edited a goal from 总览的目标管理 (OldGoal→NewGoal 文本).
//	"cancelled" — the human deleted intent IntentID (Detail = 删除原因). The intent is
//	            stopped (not deleted) and the reason is attached to it as a fact.
type TriggerEvent struct {
	Kind     string
	IntentID int64
	Detail   string
	Summary  string   // Kind=="cancelled" 专用：删除前捕获的意图摘要（真删除后节点已不存在，无法再查）
	Goals    []string // Kind=="goal" 专用：本次 set_goals 新增的目标文本（1 条或多条）
	OldGoal  string   // Kind=="goal_edited" 专用：修改前的目标文本
	NewGoal  string   // Kind=="goal_edited" 专用：修改后的目标文本
	Hints    []string // Kind=="hint" 专用：本次 add_hint 新增的提示文本（1 条或多条）
}

// renderTriggers spells out the change(s) that fired this round: for a finished
// worker — which intent + its output conclusion; for a finding — which intent +
// what was found. Empty for time/heartbeat wakes. Reads the store (best-effort;
// a blank field never blocks the round).
func renderTriggers(ts *db.ExplorationStore, evs []TriggerEvent) string {
	if len(evs) == 0 || ts == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n【이번 턴을 유발한 실제 변경 사항(먼저 읽은 뒤 방향 보완 여부를 결정)】: ")
	for _, ev := range evs {
		switch ev.Kind {
		case "goal":
			if len(ev.Goals) == 1 {
				b.WriteString(fmt.Sprintf("\n- 사람(주 에이전트)이 새 목표를 추가했습니다: %s — 새로 달성해야 할 목표입니다. 대응하는 의도가 아직 없으면 이를 바탕으로 탐색 방향을 보완하세요.", ev.Goals[0]))
			} else {
				b.WriteString(fmt.Sprintf("\n- 사람(주 에이전트)이 새 목표 %d개를 추가했습니다: %s — 모두 새로 달성해야 할 목표입니다. 대응하는 의도가 아직 없는 목표마다 탐색 방향을 보완하세요.", len(ev.Goals), strings.Join(ev.Goals, "；")))
			}
		case "hint":
			if len(ev.Hints) == 1 {
				b.WriteString(fmt.Sprintf("\n- 사람(주 에이전트)이 전략 힌트를 추가했습니다: %s — 탐색 그래프에 연결되어 있습니다. 대응하는 의도가 아직 없으면 이를 바탕으로 탐색 방향을 조정/보완하세요.", ev.Hints[0]))
			} else {
				b.WriteString(fmt.Sprintf("\n- 사람(주 에이전트)이 전략 힌트 %d개를 추가했습니다: %s — 모두 탐색 그래프에 연결되어 있습니다. 각각을 바탕으로 탐색 방향을 조정/보완하세요.", len(ev.Hints), strings.Join(ev.Hints, "；")))
			}
		case "goal_deleted":
			b.WriteString(fmt.Sprintf("\n- 사람이 이 목표를 삭제했습니다: %s — 해당 목표가 제거되었으므로 나머지 목표/방향을 다시 판단하세요. 이 목표에 더 이상 의도를 배정할 필요는 없습니다.", ev.Detail))
		case "goal_edited":
			b.WriteString(fmt.Sprintf("\n- 사람이 목표를 「%s」에서 「%s」로 변경했습니다. 새 목표에 맞게 탐색 방향을 조정하세요. 기존 방향이 더 이상 적절하지 않으면 배정을 중지하세요.", ev.OldGoal, ev.NewGoal))
		case "finding":
			b.WriteString(fmt.Sprintf("\n- 의도 #%d(%s)의 워커가 finding을 보고했습니다: %s", ev.IntentID, intentSummary(ts, ev.IntentID), ev.Detail))
		case "cancelled":
			// 意图内容优先用删除时捕获的 Summary（真删除后节点已不存在，intentSummary 查不到）。
			sm := ev.Summary
			if sm == "" {
				sm = intentSummary(ts, ev.IntentID)
			}
			b.WriteString(fmt.Sprintf("\n- 사용자가 의도 #%d를 삭제했습니다. 의도 내용: %s, 삭제 이유: %s. 해당 의도는 삭제되어 더 이상 실행하지 않습니다. 이를 바탕으로 다시 계획하세요.", ev.IntentID, sm, ev.Detail))
		default: // "done"
			b.WriteString(fmt.Sprintf("\n- 의도 #%d(%s)의 워커가 종료했습니다. 출력 결론: %s", ev.IntentID, intentSummary(ts, ev.IntentID), workerOutput(ts, ev.IntentID)))
			if fids := factIDsYielded(ts, ev.IntentID); fids != "" {
				b.WriteString(fmt.Sprintf("; 이 의도에서 새로 생성한 사실 id: %s ", fids))
			}
		}
	}
	b.WriteString("\n(전체 상세 정보는 node_detail / get_worker_output / list_findings로 다시 조회할 수 있습니다.)")
	return b.String()
}

// factIDsYielded lists the fact ids an intent produced this run as "#12、#15", so the
// planner can jump straight to the round's incremental facts. Empty (best-effort) when
// the intent yielded no facts or the lookup fails.
func factIDsYielded(ts *db.ExplorationStore, id int64) string {
	ids, err := ts.FactsYielded(id)
	if err != nil || len(ids) == 0 {
		return ""
	}
	parts := make([]string, len(ids))
	for i, fid := range ids {
		parts[i] = fmt.Sprintf("#%d", fid)
	}
	return strings.Join(parts, "、")
}

// intentSummary reads an intent node's one-line summary (best-effort, "?" on miss).
func intentSummary(ts *db.ExplorationStore, id int64) string {
	n, err := ts.GetNode(id)
	if err != nil || n == nil {
		return "?"
	}
	var p map[string]any
	if json.Unmarshal(n.Payload, &p) == nil {
		if s, ok := p["summary"].(string); ok && s != "" {
			return s
		}
	}
	return "?"
}

// workerOutput returns the finished worker's conclusion for an intent — the last
// 'result' (else 'text') activity's full detail, truncated. Same source get_worker_output uses.
func workerOutput(ts *db.ExplorationStore, id int64) string {
	acts, _, err := ts.ActivityList(&id, 0, 1000)
	if err != nil {
		return "(출력 가져오기 실패)"
	}
	var pick *db.Activity
	for i := range acts {
		if acts[i].Kind == "result" {
			pick = &acts[i]
		} else if acts[i].Kind == "text" && pick == nil {
			pick = &acts[i]
		}
	}
	if pick == nil {
		return "(이 워커의 출력 기록이 아직 없음)"
	}
	out, _ := ts.ActivityDetail(pick.ID)
	if out == "" {
		out = pick.Summary
	}
	return truncOutput(out, 800)
}

// truncOutput caps a worker-output blob so the trigger context doesn't bloat the
// system prompt every round; full text is one get_worker_output call away.
func truncOutput(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + " …(잘림, 전체 내용은 get_worker_output 참조)"
}

// renderGraphOverview folds the pre-computed graph_overview snapshot into the
// wake-up prompt so the planner starts each round with the full situation in
// hand — saving the round-trip it would otherwise spend calling the tool. It is
// the exact same JSON graph_overview would return; deeper detail is still one
// tool call away (node_detail / list_facts / …).
func renderGraphOverview(data map[string]any) string {
	b, err := json.Marshal(data)
	if err != nil {
		return "" // fall back to the model calling graph_overview itself
	}
	return "\n\n【이번 턴의 상황(graph_overview에서 미리 가져왔으며 직접 호출한 반환값과 같습니다. 세부 정보가 필요하면 node_detail/list_facts 등을 호출하세요)】: \n" + string(b)
}

// plannerDefaultTmpl is the built-in EDITABLE body (段 [A]) of the planner prompt,
// seeded into agent_prompts. Goal is a {{.Goal}} template var; the 中间产物输出规约
// tail is code-owned (artifactSpec) and appended by plannerSystem after rendering.
const plannerDefaultTmpl = `당신은 사이버 보안 플랫폼의 승인된 침투 테스트 시스템에서 "계획자" 역할을 하며 자주 호출됩니다(그래프가 바뀔 때마다 호출). 역할: 상황 읽기 → 목표 판정 → **실제로 아직 다루지 않은 새로운 방향이 있을 때만** 탐색 의도 보완. 당신은 계획자이지 실행자가 아닙니다. 이번 턴의 모든 산출물은 【의도 생성/명확화】 또는 【목표 판정】뿐이며 plan 단계에서 실행자의 일을 대신해서는 안 됩니다.

작업 목표: {{.Goal}}

**이번 턴에 생성할 의도 수(이 규칙부터 명확히 판단)**:
- **반드시 지켜야 할 최소 조건(최우선)**: 【목표 미달성】이고 【현재 open 또는 running 의도가 전혀 없음】(frontier_open=0이며 running_intents가 비어 있음)이면 이번 턴에는 목표를 향해 진행하는 의도를 하나 이상 【반드시】 생성해야 합니다. 기다릴 실행 중인 work도 대기 중인 방향도 없는 상황에서 의도 0개는 작업 정지를 뜻합니다. 알려진 방향이 모두 recent_done에만 있어도 아래의 done/exhausted/blocked 판정에 따라 새 방향 하나를 열거나 이어서 수행할 의도를 배정해야 합니다.
- 위의 최소 조건 외에는 **의도 0개도 정상적인 결과이지만 정당한 이유가 있어야 합니다**("적게 배정할수록 안정적"이라는 기본 방침이 아님). ①**이미 다루는 방향**: 생각한 방향이 모두 아직 open/running인 의도에서 처리 중입니다(말만 바꿔 기존 의도를 중복 생성하는 것은 심각한 오류). ②**의존 결과 대기**: 다음 단계가 현재 실행 중인 work의 산출물에 의존하지만 아직 결과가 나오지 않았습니다(이때 강제로 배정하면 하위 단계가 선행 결과 없이 헛돌므로, 그래프가 갱신되어 다음에 호출될 때까지 기다렸다가 배정).
- 반대로 실제로 【아직 다루지 않았고 실행 중인 work에 의존하지 않는】 새 방향이 있거나, 목표가 미달성이며 범위 안에 미검증 표면이 남아 있다면 배정해야 합니다. 의도 0개를 손쉬운 기본값으로 삼지 마세요.

**호출될 때마다 수행할 판단 절차**:

1. **전체 상황은 이 프롬프트 아래에 첨부되어 있습니다**(graph_overview 반환값이므로 다시 호출할 필요 없음): task(원래 제목+목표/루트 노드), 자산 수, goals+상태, open/running/recent_done 의도, sites_without_endpoints(엔드포인트가 없는 사이트로 탐색할 수 있는 방향을 시사), facts(탐색 사실 수이며 취약점과는 별개), recent_facts({id,summary,confidence?}).
   - **범위**: 탐색 노드(goals/의도/facts/findings)는 이 작업만 포함합니다. **자산 그래프는 전역 공유**입니다(여러 작업이 같은 그래프를 사용하고 자산 수는 범위 내 전역 집계이지 이 작업만의 수가 아님). 이 작업과 관련 없는 자산은 무시하세요.
   - **계보**: 각 의도에는 parents(상위: 어떤 사실/의도에서 파생되었는지)와 yields(하위: 어떤 사실/발견을 생성했는지)가 있고 recent_facts의 각 항목에는 from_intent가 있습니다. 이를 바탕으로 "어떤 사실이 어떤 방향에서 나왔는지, 조합하여 새 방향을 만들 수 있는지" 이해하세요.
   - **부정적/불확실한 관찰**(recent_facts의 "포트가 닫힘/인젝션 불가" 등)은 워커의 관찰이지 확정 결론이 아닙니다. 받아들이기 전에 node_detail(id)로 evidence를 확인하세요. evidence가 충분하고 confidence=observed이며 수단을 모두 시도한 경우에만 해당 방향이 잠정적으로 막혔다고 판단하세요. evidence가 없거나 "그렇게 보임/한 번만 탐지함"에 불과하거나 confidence=inferred이면 【아직 확인되지 않음】으로 처리하세요. 범위 안에 있고 다른 의도가 다루지 않으면 기본적으로 재확인 의도를 하나 배정하여 입증하거나 반박하세요(**동일한 부정적 방향은 최대 한 번만 재확인**. 재확인 후에도 부정적이고 증거가 타당하면 그 결론을 존중하고 다시 배정하지 않음).
   - **더 깊은 정보가 필요할 때만 호출**: list_facts(페이지 단위, 최신순, 기본 20개, q 필터와 before 페이지 이동, total/has_more 포함), list_findings(모든 취약점), node_detail(id)(전체 증거/상세 정보; 목록/recent_facts에는 요약만 있음), list_assets(pull: q 검색, type/company_id/task_id 필터, 페이지 이동 또는 id/ids로 직접 조회), asset_neighbors. 자산은 전역 공유이므로 기본적으로 전체를 가져오지 마세요.

2. **목표 판정(핵심 역할)**: goals에 목표와 상태가 이미 있습니다. 발견 사항이나 사실로 입증된 미달성 목표에는 prove_goal(goal_id, evidence_id, reason)을 호출하여 met로 표시하세요. **표시하는 목표가 마지막 미달성 목표이면 시스템은 전체 작업이 완료되었다고 자동 판정합니다.** 마무리는 개별 prove_goal 호출로만 이루어지며 다른 "즉시 완료" 수단은 없습니다.
   - ⚠️ **정량적 달성 기준 확인(성급한 확정 엄격히 금지)**: 목표에 정량 조건(범위 비율 X%, flag N개 확보, 특정 권한 획득)이 있으면 prove_goal 전에 위의 graph_overview 실측값(coverage.pct, findings_total 수 등)을 【반드시】 확인하세요. 기준에 미달하면 prove_goal을 【금지】하고 부족한 부분을 보완할 의도를 배정하세요. "대체로 달성/핵심은 확보"라는 이유로 met를 미리 표시해서는 안 됩니다. 예: 요구 범위 비율은 100%인데 실측 coverage.pct=40% → 미달성이므로 추가 검증 의도를 계속 배정합니다.

3. **(선택, 시작 시에만, 극히 가볍게) 이해를 위한 탐지**: 그래프에 fact가 거의 없고(recent_facts가 사실상 비어 있으며 작업이 막 시작됨) 상황만으로 초기 의도를 구체적으로 설명할 수 없을 때에만 Bash 등으로 대상에 극소수의 읽기 전용 탐지를 수행하세요(예: curl 1–2회로 홈 페이지/지문 확인). **유일하게 허용되는 산출물은 더 정확한 의도 설명 한 문장**입니다. 취약점 발견/검증/악용이나 엔드포인트/디렉터리/매개변수 열거 결과가 아닙니다(그것은 워커의 역할이므로 의도로 작성해 배정). 세 가지 엄격한 경계:
   - 그래프에 워커가 생성한 fact가 있음(facts>0 / recent_facts가 비어 있지 않음) → 직접 탐지를 더 하는 것은 【금지】합니다. 모든 판단은 기존 fact에 근거하고 이번 산출물은 "새 의도 배정" 또는 "종료"뿐입니다. 단서를 깊이 조사하려면 워커에게 의도로 배정하고 직접 curl하지 마세요.
   - 시작 시에도 탐지는 최대 3회 이하로 멈추고 초기 의도를 명확히 하는 용도로만 사용하세요. "빠르게 방향 결정"이 아니라 "심층 검증"을 하고 있음을 알게 되면(엔드포인트/디렉터리 하나씩 열거, id 하나씩 시도, 디코딩 체인, 같은 인터페이스 반복 탐지, 모든 인젝션/권한 우회/취약점 테스트 검증은 워커의 본격적인 실행 작업) 즉시 멈추고 의도로 작성하세요.
   - 기존 사실/상황에서 판단할 수 있으면 탐지할 필요 자체가 없습니다.

4. **보완할 새 방향 결정**: **여기서 "절제"란 【기존 의도를 중복하지 않음】만을 뜻하며 "가능하면 적게 배정"이 아닙니다.** 목표 미달성 시 기본 질문은 "목표에 다가가기 위해 더 깊고 강력하며 아직 다루지 않은 방법이 무엇인가"이지 "마무리할 수 있는가"가 아닙니다. 의도는 【열린 탐색 방향】이며 고정된 유형/메뉴가 아닙니다. 알려진 사실, 자산, 목표를 종합해 방향을 판단하고 open + running + recent_done과 하나씩 대조하세요.
   - 기존 open/running에서 다룸 → 다시 생성하지 않음(처리 중).
   - recent_done에 등장함 → **먼저 각 의도의 state를 읽고 어떻게 멈췄는지 구별한 뒤 결정**:
     · **done(정상 실행 완료)**: 이미 다루었으므로 같은 내용을 그대로 다시 배정하지 마세요. 막힌 경로인지는 state가 아니라 yields의 fact 결론으로 판단합니다. 【실질적으로 새로운 메커니즘】(새 사실/자산/매개변수/명백히 다른 방법)이 생겼을 때만 재배정하고 summary에 이전과의 차이를 명확히 쓰세요. 말만 바꾸거나 "다시 시도하면 될지도 모름"은 해당하지 않으며 재시도는 금지합니다.
     · **exhausted(예산 소진, 탐색 도중 중단되어 일부만 기록) / blocked(모델/네트워크 실패로 사실상 탐색하지 못함)**: 중간에 정상 종료하지 못하여 정보가 불완전합니다. 먼저 get_worker_trace / get_worker_output으로 실제로 어디까지 했고 어디에 막혔는지 확인한 뒤 선택하세요. 돌파 직전에 예산이 끝남 → "이전 진행 지점부터 이어가기" 배정. 순수한 외부 장애로 실행하지 못함(blocked가 흔히 해당) → 같은 방향을 바로 재배정. 매번 같은 곳에서 막힘 → 방법/방향 변경. 근거는 언제나 trace의 실제 진행 상황이지 state 자체가 아닙니다.
   - 어떤 의도도 다루지 않은 완전히 새로운 방향 → 생성.
   - 알려진 모든 방향을 아직 open/running인 의도가 다룸 → 생성하지 않고 즉시 종료(실행 중/대기 중인 work가 있으므로 진행을 기다림). 하지만 recent_done만 있고 open/running이 없으며 목표 미달성이라면 위의 최소 조건에 따라 새 방향을 열거나 이어서 수행할 의도를 반드시 배정하세요.
   - **깊이가 범위 비율보다 우선**: coverage는 하한/달성 기준이지 탐색 목표 자체가 아닙니다. 중요한 진입점(RCE/권한 상승/데이터 유출로 이어질 가능성)을 발견하면 자산마다 얕게 테스트하여 범위 비율을 고르게 늘리기보다 그 경로를 【더 깊이 진행】할 의도를 우선하세요.
   - **경로 다양성을 유지하고 너무 일찍 수렴하지 않기**: 목표가 미달성인데 기존 의도가 모두 같은 경로/진입점에 몰려 있고 【본질적으로 다른】 미검증 방향(다른 진입 표면/자산 종류/악용 체인)이 있으면 같은 경로의 동의어 의도를 추가하지 말고 다른 방향을 우선 보완하세요(표현이 아니라 실질적인 차이를 판단). 다른 방향도 기존 의도가 다루고 있다면 여전히 생성하지 않습니다. 이상적인 상태는 메커니즘이 다른 2–3개 경로가 공존하는 것(예: "업로드 체인 경로"와 "인증 우회 경로")이며, 어떤 경로가 【목표에 가까워짐】을 입증한 뒤에만 자원을 집중하세요. **그러나 다양성은 항상 최상위 【동작 제약】을 따릅니다.** 제약으로 제외한 진입 표면/포트/호스트/동작은 본질적으로 다르더라도 의도를 절대 생성하지 마세요.

   **순차 악용 체인: 단계별 배정, 병렬로 나누지 않기.** 강한 의존성이 있는 순차 경로(①→②→③, 뒤 단계가 앞 단계의 실제 산출물에 의존)는 한꺼번에 병렬 배정하지 마세요(아직 없는 선행 결과를 받지 못해 중복하거나 헛돎). TodoWrite에 전체 경로를 단계당 한 항목으로 기록하고 이번 턴에는 "선행 조건이 충족된" 단계(보통 첫 단계)만 배정하세요. 해당 단계가 fact를 산출한 뒤 다음 호출에서(할 일 목록이 프롬프트에 포함됨) 다음 단계를 배정하고 충족된 항목은 completed로 표시하세요. "같은 일"을 두 개로 나누지 마세요("유발 지점 확인"과 "그 지점 유발"은 같은 단계). 【병렬이며 상호 의존하지 않는】 방향(예: 관련 없는 여러 엔드포인트 열거)만 여러 의도로 병렬 배정하세요.

5. **제출**: 선별한 새 방향을 add_intent 【한 번】으로 일괄 제출하세요(intents 배열, 가치가 가장 높은 최대 4개, 하나씩 여러 번 호출하지 않음).
   - **summary**: 방향을 자연어 한 문장으로 설명하세요(테스트 대상 전체 주소 + 무엇을 + 왜). 고정 분류에 맞추지 마세요. 기존 의도와의 중복 판정은 주로 이 설명에 근거합니다.
   - **asset_ids**: 이 방향에서 테스트/공격할 대상 자산 id(list_assets에서 가져온 0/1/여러 개, 최대한 제공). 구체적인 자산(사이트/인터페이스/매개변수/호스트)에 관한 방향이면 반드시 전달하여 범위 중복 제거와 자산 경로 연결에 사용하세요. 여러 자산에 걸치면 모두 전달하며, 구체적인 자산이 없는 순수 전역 정찰만 비워 두세요.
   - **parent_ids**: 이 방향을 어떤 상위 노드에서 종합했는지(선택, 0/1/여러 개). 여러 사실을 결합하여 의도를 만들었으면 모두 전달하고, 상위 의도/발견에서 파생되었으면 그 id도 전달하세요. 최상위의 완전히 새로운 방향이면 비워 두세요.

중복하거나 억지로 채우지 마세요. 그러나 목표 미달성이며 더 깊고 아직 다루지 않은 방법이 있으면 배정해야 합니다. 간결하고 집중적이며 효율적으로 행동하세요.`

func plannerSystem(goal, dataDir, workDir string) string {
	body := renderSystem("planner", plannerDefaultTmpl, PlannerVars{Goal: goal, DataDir: dataDir, Now: nowStr()})
	return body + artifactSpec(workDir)
}

// Plan runs one planning round. emit, if non-nil, receives the planner's execution
// steps (so users can see how it reads the situation and judges goals — the
// planner is the intent generator and was previously a black box). Returns whether
// the planner judged the goal met.
// triggers carries the concrete change(s) that fired this round — worker(s) done
// and/or finding(s) reported (may be several — the engine debounces a burst; empty
// for time/heartbeat wakes). They are spelled out at the top of the prompt so the
// planner looks first at the actual change (which intent, its output/finding).
func (p *Planner) Plan(ctx context.Context, taskID int64, as *db.AssetStore, ts *db.ExplorationStore, goal string, triggers []TriggerEvent, emit func(db.Activity)) (met bool, reason string, err error) {
	// cold-digest §2.3/§7: advance this task's planner-round counter, maintain the
	// cold_since_round stamps, and (if a threshold is hit) kick off background
	// compaction. Synchronous part is cheap (a few queries); the LLM compaction
	// runs in a detached goroutine so it never adds latency to this round.
	p.compactor.OnPlannerRound(ctx, ts)
	tsx := NewToolSet(ts, "planner")
	tsx.SetFindingRecorder(p.findingRecorder)
	if as != nil {
		tsx.SetAssetStore(as, as.Companies())
	}
	tsx.SetTaskID(taskID)
	tsx.SetCoverageEnabled(as == nil || as.CoverageEnabled(taskID))
	tsx.killWork = p.killWork   // enable kill_work tool (nil = unavailable)
	tsx.steerWork = p.steerWork // enable steer_work tool (nil = unavailable)
	if origin, _ := ts.OriginFactID(); origin > 0 {
		tsx.SetOwnerNode(origin) // planner-side anchors default to the task root (origin fact)
	}
	// 领域工具 + 基础默认工具集（Read/Write/Edit/MultiEdit/LS/Glob/Grep/Bash）
	// 资产覆盖度功能关闭时剔除 add_task_scope/list_untested_assets（不入 prompt）。
	base := append(tsx.DropCoverageTools(tsx.PlannerTools()), actool.DefaultTools()...)
	ctx = WithRunInfo(ctx, RunInfo{TaskID: taskID, ExplorationID: explorationID(ts)})
	tools, def, cleanup := AugmentTools(ctx, "planner", base)
	defer cleanup()
	// 关键态势（刚完成的意图 + 预取的完整图）改放【本轮 user 输入】(见下方 input)，system
	// 只留静态规划正文。move-out 让 system 每轮稳定、更利于缓存；代价是若单轮变长，态势可能
	// 被 compaction 压缩（planner 单轮通常短，风险低）。situational 会拼进下方 input。
	situational := renderTriggers(ts, triggers) + renderGraphOverview(tsx.graphOverviewData())
	// 任务级 deadline / 终局模式(经 ctx 注入,见 taskclock.go)。终局那一轮把任务超时
	// planner 收尾词作为【本轮操作指令】拼进本轮 user 输入(随 situational),让它只做最后
	// 目标判定、不产新意图。
	tc := taskClockFrom(ctx)
	if tc.Final {
		situational += "\n\n【작업 최종 마무리(이번 턴의 특별 지시이며 위의 일반 계획 절차보다 우선)】: " + resolveTaskTimeoutWrapup("planner")
	}
	// 本任务的工作目录 <workDir>/tasks/<taskID>，先建好。
	taskDir := ensureRunDir(p.workDir, taskID, 0)
	ctx = intercept.WithReviewContext(ctx, taskDir, intercept.ReviewBackground{})
	sysBody := plannerSystem(goal, p.workDir, taskDir)
	if p.wantConstraints() {
		sysBody += constraintBlock(ts) // 操作约束(若有)注入系统提示,框定探索边界
	}
	system, boundary := deferredSystem(sysBody, def)
	// planner 无自身墙钟预算;有 deadline 时把 MaxDuration 夹逼到剩余,让在跑的规划轮在
	// 任务到点时进收尾(因超时→任务超时词,因步数→per-run 词)。
	maxDur, clamped := clampMaxDuration(tc.DeadlineUnix, 0)
	settle := wrapupSettlement("planner", nil)
	if tc.DeadlineUnix > 0 {
		settle = wrapupSettlementForTask("planner", nil, clamped)
	}
	opts := agentcore.Options{
		Provider:        p.prov,
		SystemPrompt:    system,
		DynamicBoundary: boundary,
		Tools:           tools,
		DeferredTools:   def.Deferred,
		UnlockSet:       def.Unlock,
		PermissionMode:  permission.ModeBypass,
		EnableWebFetch:  true, // 走记录代理留痕；载入代理 CA 验证 MITM 重签的 HTTPS 证书
		WebFetchProxy:   p.proxyAddr,
		WebFetchCACert:  p.proxyCACert,
		// 联网搜索(可选)。ddgs 无需 key；brave-free 需 BraveKey；tavily 需 TavilyKey。
		// WebSearchProxy 是独立出口代理(http/https/socks5)，与记录流量的 MITM 代理无关；空则直连。
		EnableWebSearch:       p.webSearch.Enabled,
		WebSearchBackend:      p.webSearch.Backend,
		BraveSearchAPIKey:     p.webSearch.BraveKey,
		TavilySearchAPIKey:    p.webSearch.TavilyKey,
		DeepSeekSearchBaseURL: p.webSearch.DeepSeekBaseURL,
		DeepSeekSearchAPIKey:  p.webSearch.DeepSeekAPIKey,
		DeepSeekSearchModel:   p.webSearch.DeepSeekModel,
		WebSearchProxy:        p.webSearch.Proxy,
		BashEnv:               proxyEnv(p.proxyAddr, p.proxyCACert), // Bash 子命令默认走代理+信任 CA
		WorkingDir:            taskDir,                              // 本任务工作目录 <workDir>/tasks/<taskID>
		ToolOutputDir:         cmdOutDir(taskDir),
		MaxTurns:              p.maxTurns, // 0 = unlimited (configurable in agent management)
		MaxDuration:           maxDur,     // 0=不限;有 deadline 时=距 deadline 剩余
		Compaction:            compactionConfig(p.compactionWindow()),
		// 跨唤醒共享的规划待办：让串行链在多轮之间保留（session 是新的，store 不是）。
		Todos: p.todoFor(ts.ID()),
		// 命中【本轮】步数预算→ SDK 跑收尾:把本轮已想清楚的结论落地(该派的 add_intent、
		// 能证的 prove_goal、串行链记 TodoWrite),而非停止规划——planner 之后仍会被反复唤醒。
		// clamped(被任务 deadline 夹逼)时改用 PromptByReason(见 wrapupSettlementForTask)。
		Settlement:   settle,
		NonStreaming: p.nonStreaming(), // 该 profile 选非流式时走 Provider.Complete
		MaxTokens:    p.maxTokens(),    // 0 = 不发上限,由服务端默认值决定
	}
	if p.tx != nil { // persist raw LLM conversation; one accumulating file per task's planner
		opts.Transcript = p.tx
		opts.SessionID = fmt.Sprintf("exp%d-planner", ts.ID())
	}
	// 实验功能:开启后由 noa 接管上下文压缩(归档集中在 <workDir>/noa/<SessionID> 下,持久)。
	noaSession := fmt.Sprintf("exp%d-planner", ts.ID())
	enableNoa(&opts, p.noaEnabledFn, p.workDir, noaSession, noaWarn(noaSession))
	// 态势（刚完成的意图 + 完整图）现在拼进本轮 user 输入（见下方 input）。user 里还有
	// 指令 + 跨唤醒待办（todo 是模型自己的规划便签，可再生，放 user 即可）。
	// 开场白按「本轮有无具体变动」分两种：有变动 → 指向下方【实际变动】块；无变动
	// (心跳定时巡检 / hint / 恢复等) → 别谎称"图发生了变化",转而提示顺带复查在跑意图。
	lead := "구체적인 변경 사항이 발생했습니다(아래 【이번 턴을 유발한 실제 변경 사항】 참조). 이를 바탕으로 다음 단계를 계획하세요: "
	if len(triggers) == 0 {
		lead = "이번 호출은 **주기적 점검(주기 도달) / 구체적인 변경 신호 없음**에 의한 것이므로 그래프에 새 변경이 없을 수도 있습니다. 실행 중인 의도도 함께 확인하세요. 오랫동안 진행이 없거나 방향이 어긋난 경우 steer_work로 바로잡고, 전체 방향이 틀렸다면 kill_work로 중지하세요. 이어서 목표를 판정하고 방향 보완 여부를 결정하세요: "
		// 心跳/无变动唤醒时,若全图已无任何 open 或 running 意图 → 探索已停摆(没 worker 在跑、
		// 也没排队方向)。明确告知 planner 并强制其本轮补出新方向,别只复查在跑意图后空转一轮。
		if active, err := ts.HasActiveIntent(); err == nil && !active {
			lead = "이번 호출은 **주기적 점검(주기 도달)**이며 현재 **open 또는 running 의도가 전혀 없습니다**. 실행 중인 워커도 대기 중인 방향도 없어 탐색이 멈췄습니다. 이번 턴에 목표를 향해 진행하고 그래프의 기존 의도와 **중복되지 않는** 새 의도를 하나 이상 **반드시** 생성해야 합니다(의도 0개는 불가). 아래 상황을 근거로 먼저 목표 달성 여부를 판정하고 미달성이면 즉시 방향을 보완하세요: "
		}
	}
	input := lead + situational + "\n\n위의 상황을 바탕으로 목표를 판정하세요. 목표가 【실제로 달성됨】(목표 산출물 확보 / 목표 취약점 확인)이면 prove_goal로 하나씩 표시하세요. **반드시 지켜야 할 최소 조건: 목표가 아직 미달성이고 현재 open 또는 running 의도가 전혀 없으면(frontier_open=0이고 running_intents가 비어 있음), 이번 턴에는 목표를 향해 진행할 의도를 하나 이상 반드시 생성해야 합니다. 기다릴 실행 중인 work도 대기 중인 방향도 없으므로 의도 0개는 작업 정지를 뜻합니다. 기존 open/running 의도가 진행 중이거나 목표가 달성된 경우에만 새 의도를 생성하지 않아도 됩니다.**" +
		renderPlannerTodos(opts.Todos.List())
	// MaxDuration 现在会在墙钟到点打断在跑工具并就地进收尾(在活 ctx 上),单轮卡死不再
	// 绕过收尾,无需外部硬 ctx 兜底。ctx 只承载 pause / kill / shutdown。
	_, _, err = captureRun(ctx, opts, input,
		func(r db.Activity) {
			if emit != nil {
				r.Worker = "planner" // planner activity has no intent_id (it generates them)
				emit(r)
			}
		})
	return tsx.GoalMet, tsx.Reason, err
}
