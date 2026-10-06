package agent

import (
	"encoding/json"
	"fmt"

	"github.com/Autumn-27/artex/db"
	actool "github.com/Autumn-27/norma/tool"
)

const findingIDGuidance = "\n\n**취약점 ID 규칙**: finding_id는 독립 취약점 기록 ID이며 finding_node_id는 탐색 노드 ID입니다. list_findings / list_task_findings / node_detail / get_task_node_detail의 id는 계속 탐색 노드 ID이므로 독립 ID는 같은 반환 결과의 finding_id에서 읽으세요. get_finding_traffic / bind_finding_traffic에는 독립 finding_id를 사용합니다. 기존 update_finding_report의 finding_id 인수에는 여전히 finding_node_id를 전달합니다. report_finding 첫 줄의 숫자를 증거 도구에 사용하지 말고, ID 오류가 발생해도 다른 숫자를 추측하지 마세요."

// The server supplies the persisted setting. A missing setting/host is off.
// Consulted at assembly and again on writes so an already-running session
// cannot keep binding after the user switches the feature off.
var FindingTrafficBindingEnabled func() bool

func findingTrafficBindingEnabled() bool {
	return FindingTrafficBindingEnabled != nil && FindingTrafficBindingEnabled()
}

// Applied after ToolResolve: user descriptions and prompts remain intact, while
// all actual reporters (including Planner and custom chat agents) see the same
// API contract. Disabled/unbound tools are never reintroduced here.
func findingWorkflowTools(agentKey string, tools []actool.CoreTool) ([]actool.CoreTool, string) {
	if !findingTrafficBindingEnabled() {
		out := make([]actool.CoreTool, 0, len(tools))
		for _, tool := range tools {
			if tool.Name() == "bind_finding_traffic" {
				continue
			}
			if agentKey == "reporter" && (tool.Name() == "traffic_search" || tool.Name() == "traffic_get" || tool.Name() == "traffic_blob") {
				continue
			}
			switch tool.Name() {
			case "report_finding", "add_hint", "add_task_hint":
				// Work on a copy: toggling back on must restore the original schema.
				raw, _ := json.Marshal(tool.InputSchema())
				var schema map[string]any
				if json.Unmarshal(raw, &schema) == nil {
					stripTrafficParameters(schema)
					tool = DecorateTool(tool, tool.Description(), schema)
				}
			}
			out = append(out, tool)
		}
		return out, ""
	}
	out := append([]actool.CoreTool(nil), tools...)
	has := map[string]bool{}
	for i, tool := range out {
		has[tool.Name()] = true
		note := ""
		switch tool.Name() {
		case "report_finding":
			note = "\n기본적으로 보고서 에이전트가 보고서 작성 전에 트래픽을 확인하여 연결합니다. 보고자는 evidence에 검증 명령, 주요 출력, 이미 확보한 실제 트래픽 ID와 용도를 보존하여 보고서 에이전트가 실행 기록과 대조할 수 있게 하세요. 연결만을 위해 추가로 패킷을 조회할 필요는 없습니다. 명시적인 즉시 연결도 호환됩니다. traffic_refs 또는 evidence_hint_id로 검증된 참조를 제출할 수 있으며, 후자는 이 작업에서 지정한 hint의 구조화된 참조를 읽습니다. 하나라도 유효하지 않으면 이번 보고 전체가 실패합니다. TCP/패킷 없음에는 이 선택 인수가 필요 없습니다. 반환하는 finding_id와 finding_node_id는 각각 독립 기록과 탐색 노드를 뜻합니다."
		case "add_hint", "add_task_hint":
			note = "\n확인된 취약점을 전달할 때 해당 힌트의 traffic_refs에 검증된 트래픽의 ID, 용도, 설명과 순서를 보존하세요(단일 항목은 최상위, 일괄 항목은 해당 hints 요소에 배치). text에는 그 트래픽이 입증하는 구체적인 취약점을 설명하세요. 호출자는 텍스트만 전달하면서 기존 트래픽 참조를 버려서는 안 됩니다. 검증하지 않은 후보를 증거로 전달해서는 안 됩니다."
		case "get_finding_traffic", "bind_finding_traffic", "list_findings", "list_task_findings", "node_detail", "get_task_node_detail", "update_finding_report":
			note = findingIDGuidance
		}
		if note != "" {
			out[i] = DecorateTool(tool, tool.Description()+note, tool.InputSchema())
		}
	}
	guidance := ""
	if has["report_finding"] || has["add_task_hint"] || has["add_hint"] {
		guidance = "\n\n**트래픽 증거 전달(선택)**: 자동 연결은 기본적으로 취약점 저장 후 보고서 작성 전에 보고서 에이전트가 수행합니다. 보고자는 evidence에 검증 명령, 주요 출력, 기존 실제 트래픽 ID와 용도를 보존하고 작업에서는 intent_id도 포함하여 보고서 에이전트가 추적할 수 있게 하세요. 연결만을 위해 추가로 패킷을 조회할 필요는 없습니다. Auto / Planner가 대신 보고할 때도 실행자가 확보한 참조를 버리지 마세요. add_hint / add_task_hint는 traffic_refs로 전달할 수 있습니다. report_finding의 traffic_refs / evidence_hint_id를 통한 명시적인 즉시 연결도 계속 지원합니다. TCP이거나 패킷이 없어도 정상적으로 등록하세요. ID를 추측하거나 패킷을 보충하기 위해 탐지를 반복해서는 안 됩니다."
		if has["add_task_hint"] && !has["add_hint"] {
			guidance += "\n플랫폼 대화에 작업 컨텍스트가 없으면 report_finding을 직접 호출하지 마세요. add_task_hint로 기존의 해당 작업에 전달하고 작업 에이전트가 등록하도록 한 뒤 list_task_findings로 결과를 확인하세요."
		}
		if has["prove_goal"] || has["goal_met"] {
			guidance += "\n목표 달성을 판정하기 전에 이번에 이미 확보한 증거의 보고/전달을 먼저 완료하세요. 증거 전달이 끝나지 않았는데 텍스트 취약점을 등록했다는 이유만으로 작업을 종료하고 Worker를 취소하지 마세요. 패킷이 없을 때는 기다리거나 강제로 캡처할 필요가 없습니다."
		}
	}
	if has["update_finding_report"] && has["bind_finding_traffic"] && has["get_finding_traffic"] {
		guidance += "\n\n**보고서 작성 전 트래픽 자동 연결(활성화됨)**: 당신은 이번에 트리거된 취약점의 트래픽을 확인하여 연결한 뒤 보고서를 작성해야 합니다. 먼저 report_finding의 반환 JSON 또는 get_task_node_detail / list_task_findings에서 명확한 finding_id와 finding_node_id를 얻으세요. 취약점 상세 정보, 해당 의도의 실행 기록 및 기존 증거 목록을 읽고 보고자가 전달한 실제 ID를 우선 사용하세요. 이번 검증이 HTTP이고 트래픽 도구를 사용할 수 있으면 traffic_search로 후보를 찾고 traffic_get으로 요청/응답이 실제로 이 취약점을 뒷받침하는지 하나씩 검증하세요. 도메인과 시간은 필터에만 사용하며 소속 관계의 증거가 아닙니다. 확인한 증거를 재현 순서대로 bind_finding_traffic(finding_id, traffic_refs)에 연결하고 baseline / proof / verification / supporting 중 용도를 선택하여 설명하세요. 이번 취약점만 처리하며 취약점을 중복 생성하거나 대상을 다시 탐지하지 마세요. 연결에 성공하면 get_finding_traffic을 다시 호출하여 최신 version을 얻고 필요한 본문을 읽은 다음, 실제로 읽은 version을 evidence_version으로 update_finding_report에 전달하세요(이 도구의 finding_id 인수에는 여전히 finding_node_id 사용). 기존 연결을 중복 추가할 필요는 없습니다. TCP, 미캡처, 도구 사용 불가 또는 정확한 일치 항목이 없을 때는 자동 연결을 건너뛰고 텍스트/명령 증거로 정상적으로 보고서를 작성하며 이유를 설명하세요. 트래픽을 억지로 채우기 위해 추측해서는 안 됩니다. 연결에 실패했다면 성공했다고 말하지 마세요. 기존 증거를 보존하고 보고서에 연결하지 못한 이유를 설명하세요."
	}
	if guidance != "" || has["get_finding_traffic"] || has["update_finding_report"] {
		guidance += findingIDGuidance
	}
	return out, guidance
}

func stripTrafficParameters(schema map[string]any) {
	props, _ := schema["properties"].(map[string]any)
	delete(props, "traffic_refs")
	delete(props, "evidence_hint_id")
	if required, ok := schema["required"].([]any); ok {
		kept := required[:0]
		for _, key := range required {
			if key != "traffic_refs" && key != "evidence_hint_id" {
				kept = append(kept, key)
			}
		}
		schema["required"] = kept
	}
	if hints, ok := props["hints"].(map[string]any); ok {
		if items, ok := hints["items"].(map[string]any); ok {
			stripTrafficParameters(items)
		}
	}
}

// HintTrafficSchema is shared by the task-local and cross-task hint tools.
func HintTrafficSchema() map[string]any {
	return map[string]any{"type": "array", "description": "선택: 검증되었으며 이 힌트의 구체적인 취약점에 해당하는 트래픽 참조입니다. 순서를 보존하세요. 전달 후 report_finding에 evidence_hint_id를 제공하여 이 참조를 포함할 수 있습니다.", "items": obj(map[string]any{"traffic_id": str("실제 트래픽 ID"), "role": str("baseline / proof / verification / supporting"), "note": str("이 트래픽이 뒷받침하는 결론")}, "traffic_id")}
}

func (t *ToolSet) findingRefsFromHint(hintID int64, explicit []db.TrafficRef) ([]db.TrafficRef, error) {
	if hintID <= 0 {
		return db.NormalizeTrafficRefs(explicit)
	}
	n, err := t.ts.GetNode(hintID) // local store only: inherited hints cannot supply evidence
	if err != nil {
		return nil, err
	}
	if n == nil || n.Kind != db.KindHint {
		return nil, fmt.Errorf("evidence_hint_id=%d는 이 작업의 힌트 노드여야 합니다(상속한 힌트는 직접 연결에 사용할 수 없음)", hintID)
	}
	var payload struct {
		Refs []db.TrafficRef `json:"traffic_refs"`
	}
	if err := json.Unmarshal(n.Payload, &payload); err != nil {
		return nil, err
	}
	return db.NormalizeTrafficRefs(append(append([]db.TrafficRef{}, explicit...), payload.Refs...))
}
